import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import worker, {
	DEFAULT_MIRROR_ORIGIN,
	DEFAULT_RELEASE_BASE,
	GEOX_DOWNLOAD,
	MIHOMO_API,
	MIHOMO_DOWNLOAD,
	mirrorBase,
	rewriteInstallScript,
	rewriteMihomoRelease,
	routeRequest,
} from "./worker.js";

const installSource = readFileSync(new URL("../install.sh", import.meta.url), "utf8");

test("install.sh default base matches the worker rewrite target", () => {
	assert.match(installSource, new RegExp(DEFAULT_RELEASE_BASE.replaceAll(".", "\\.")));
});

test("rewriteInstallScript points the default base at the request host", () => {
	const rewritten = rewriteInstallScript(installSource, "mirror.example.com");
	assert.match(
		rewritten,
		/PARTYCTL_BASE_URL="\$\{PARTYCTL_BASE_URL:-https:\/\/mirror\.example\.com\}"/,
	);
	assert.equal(rewritten.includes(DEFAULT_RELEASE_BASE), false);
	assert.equal(rewritten.includes(DEFAULT_MIRROR_ORIGIN), false);
	assert.match(rewritten, /PARTYCTL_MIRROR:-https:\/\/mirror\.example\.com/);
	assert.equal(rewriteInstallScript(installSource, 'evil".example'), installSource);
	assert.equal(mirrorBase("partyctl.example.com:8443"), "https://partyctl.example.com:8443");
});

test("routes map install and release downloads", () => {
	assert.equal(routeRequest("/").kind, "install");
	assert.equal(routeRequest("/install.sh").kind, "install");
	assert.deepEqual(routeRequest("/latest/download/partyctl-linux-amd64.tar.gz"), {
		kind: "asset",
		upstream: `${DEFAULT_RELEASE_BASE}/latest/download/partyctl-linux-amd64.tar.gz`,
		cacheControl: "public, max-age=300",
	});
	assert.deepEqual(routeRequest("/download/v0.1.0/SHA256SUMS"), {
		kind: "asset",
		upstream: `${DEFAULT_RELEASE_BASE}/download/v0.1.0/SHA256SUMS`,
		cacheControl: "public, max-age=31536000, immutable",
	});
	assert.equal(routeRequest("/latest/download/..").kind, "bad");
	assert.equal(routeRequest("/latest/download/../secret").kind, "notfound");
	assert.equal(routeRequest("/download/not-a-tag/file").kind, "bad");
	assert.equal(routeRequest("/nope").kind, "notfound");
	assert.deepEqual(routeRequest("/mihomo/releases/latest"), {
		kind: "mihomo-api",
		upstream: `${MIHOMO_API}/latest`,
		cacheControl: "public, max-age=300",
	});
	assert.equal(routeRequest("/mihomo/releases/tags/v1.19.33").upstream, `${MIHOMO_API}/tags/v1.19.33`);
	assert.equal(routeRequest("/mihomo/releases/tags/latest").kind, "bad");
	assert.equal(routeRequest("/mihomo/download/v1.2.3/mihomo-linux-amd64-v1.2.3.gz").upstream,
		`${MIHOMO_DOWNLOAD}/v1.2.3/mihomo-linux-amd64-v1.2.3.gz`);
	assert.equal(routeRequest("/mihomo/download/../secret").kind, "bad");
	assert.equal(routeRequest("/github/MetaCubeX/mihomo/releases/latest").kind, "notfound");
	assert.equal(routeRequest("/geox/download/latest/geoip.dat").upstream, `${GEOX_DOWNLOAD}/latest/geoip.dat`);
	assert.equal(routeRequest("/geox/download/latest/secrets.txt").kind, "notfound");
	assert.equal(routeRequest("/geox/download/not a tag/geoip.dat").kind, "bad");
});

test("GET /install.sh rewrites the script and falls back to raw main", async () => {
	const calls = [];
	const original = globalThis.fetch;
	globalThis.fetch = async (url) => {
		calls.push(String(url));
		if (String(url).endsWith("/latest/download/install.sh")) {
			return new Response("missing", { status: 404 });
		}
		assert.equal(String(url), "https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh");
		return new Response(installSource, {
			status: 200,
			headers: { "Content-Type": "text/plain" },
		});
	};
	try {
		const res = await worker.fetch(
			new Request("https://mirror.example.com/install.sh"),
		);
		assert.equal(res.status, 200);
		assert.match(res.headers.get("Cache-Control"), /max-age=300/);
		const body = await res.text();
		assert.match(body, /https:\/\/mirror\.example\.com\}/);
		assert.equal(body.includes(DEFAULT_RELEASE_BASE), false);
		assert.deepEqual(calls, [
			`${DEFAULT_RELEASE_BASE}/latest/download/install.sh`,
			"https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh",
		]);
	} finally {
		globalThis.fetch = original;
	}
});

test("GET /latest/download proxies the GitHub body and sets a short cache", async () => {
	const original = globalThis.fetch;
	globalThis.fetch = async (url) => {
		assert.equal(
			String(url),
			`${DEFAULT_RELEASE_BASE}/latest/download/partyctl-linux-amd64.tar.gz`,
		);
		return new Response("tar-bytes", {
			status: 200,
			headers: {
				"Content-Type": "application/octet-stream",
				"Content-Length": "9",
			},
		});
	};
	try {
		const res = await worker.fetch(
			new Request("https://mirror.example.com/latest/download/partyctl-linux-amd64.tar.gz"),
		);
		assert.equal(res.status, 200);
		assert.equal(await res.text(), "tar-bytes");
		assert.equal(res.headers.get("Cache-Control"), "public, max-age=300");
		assert.equal(res.headers.get("Content-Type"), "application/octet-stream");
	} finally {
		globalThis.fetch = original;
	}
});

test("GET /download/<tag>/<file> uses a long cache and streams the body", async () => {
	const original = globalThis.fetch;
	globalThis.fetch = async () =>
		new Response("sums", {
			status: 200,
			headers: { "Content-Type": "text/plain" },
		});
	try {
		const res = await worker.fetch(
			new Request("https://mirror.example.com/download/v0.1.0/SHA256SUMS"),
		);
		assert.equal(res.status, 200);
		assert.equal(res.headers.get("Cache-Control"), "public, max-age=31536000, immutable");
		assert.equal(await res.text(), "sums");
	} finally {
		globalThis.fetch = original;
	}
});

test("mihomo release JSON is rewritten onto the mirror and evil redirects are refused", async () => {
	const original = globalThis.fetch;
	const payload = JSON.stringify({
		tag_name: "v1.2.3",
		assets: [{
			name: "mihomo-linux-amd64-v1.2.3.gz",
			browser_download_url: `${MIHOMO_DOWNLOAD}/v1.2.3/mihomo-linux-amd64-v1.2.3.gz`,
		}],
	});
	globalThis.fetch = async (url) => {
		assert.equal(String(url), `${MIHOMO_API}/latest`);
		return new Response(payload, { status: 200, headers: { "Content-Type": "application/json" } });
	};
	try {
		const res = await worker.fetch(new Request("https://get.251819788.xyz/mihomo/releases/latest"));
		assert.equal(res.status, 200);
		const body = await res.json();
		assert.equal(
			body.assets[0].browser_download_url,
			"https://get.251819788.xyz/mihomo/download/v1.2.3/mihomo-linux-amd64-v1.2.3.gz",
		);
		assert.equal(rewriteMihomoRelease("not-json", "get.251819788.xyz"), "not-json");
	} finally {
		globalThis.fetch = original;
	}

	globalThis.fetch = async () =>
		new Response(null, { status: 302, headers: { Location: "https://evil.example/steal" } });
	try {
		const res = await worker.fetch(
			new Request("https://get.251819788.xyz/mihomo/download/v1.2.3/mihomo-linux-amd64-v1.2.3.gz"),
		);
		assert.equal(res.status, 502);
	} finally {
		globalThis.fetch = original;
	}
});

test("mihomo asset follows a GitHub CDN redirect", async () => {
	const original = globalThis.fetch;
	let hop = 0;
	globalThis.fetch = async (url) => {
		hop += 1;
		if (hop === 1) {
			assert.equal(String(url), `${MIHOMO_DOWNLOAD}/v1.2.3/mihomo-linux-amd64-v1.2.3.gz`);
			return new Response(null, {
				status: 302,
				headers: { Location: "https://objects.githubusercontent.com/a.gz" },
			});
		}
		assert.equal(String(url), "https://objects.githubusercontent.com/a.gz");
		return new Response("gzip-bytes", { status: 200, headers: { "Content-Type": "application/gzip" } });
	};
	try {
		const res = await worker.fetch(
			new Request("https://get.251819788.xyz/mihomo/download/v1.2.3/mihomo-linux-amd64-v1.2.3.gz"),
		);
		assert.equal(res.status, 200);
		assert.equal(await res.text(), "gzip-bytes");
		assert.equal(res.headers.get("Cache-Control"), "public, max-age=86400");
	} finally {
		globalThis.fetch = original;
	}
});

test("rejects unsupported methods and unknown paths", async () => {
	const post = await worker.fetch(new Request("https://mirror.example.com/", { method: "POST" }));
	assert.equal(post.status, 405);
	const missing = await worker.fetch(new Request("https://mirror.example.com/nope"));
	assert.equal(missing.status, 404);
});
