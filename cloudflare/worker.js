const GITHUB_REPO = "erhao007/partyctl";

// Default release base baked into install.sh. The mirror rewrites this to https://<host>.
const DEFAULT_RELEASE_BASE = `https://github.com/${GITHUB_REPO}/releases`;

// Default kernel/geox mirror baked into install.sh. Rewritten to the request host.
const DEFAULT_MIRROR_ORIGIN = "https://get.251819788.xyz";

// Version lookup uses the public release page, not api.github.com.
// GitHub's REST API often answers 403 to unauthenticated Cloudflare egress.
const MIHOMO_LATEST_PAGE = "https://github.com/MetaCubeX/mihomo/releases/latest";
const MIHOMO_TAG_PAGE = "https://github.com/MetaCubeX/mihomo/releases/tag/";
const MIHOMO_DOWNLOAD = "https://github.com/MetaCubeX/mihomo/releases/download";
const GEOX_DOWNLOAD = "https://github.com/MetaCubeX/meta-rules-dat/releases/download";

// Workers runtime rejects string exports. Tests read these through a function.
export function workerConstants() {
	return {
		DEFAULT_RELEASE_BASE,
		DEFAULT_MIRROR_ORIGIN,
		GEOX_DOWNLOAD,
		MIHOMO_DOWNLOAD,
		MIHOMO_LATEST_PAGE,
	};
}

const MIHOMO_TAG = /^v[0-9A-Za-z._-]+$/;
const ASSET_NAME = /^[A-Za-z0-9._-]+$/;
const GEOX_TAG = /^(?:latest|v[0-9A-Za-z._-]+)$/;
const GEOX_FILES = new Set([
	"geoip.dat",
	"geosite.dat",
	"geoip.metadb",
	"GeoLite2-ASN.mmdb",
]);

const RAW_INSTALL_URL = `https://raw.githubusercontent.com/${GITHUB_REPO}/main/install.sh`;
const RELEASE_INSTALL_URL = `${DEFAULT_RELEASE_BASE}/latest/download/install.sh`;

const UA = "partyctl-mirror";

function safeToken(value, pattern) {
	if (typeof value !== "string" || value === "." || value === "..") return null;
	if (!pattern.test(value)) return null;
	return value;
}

export function mirrorBase(host) {
	if (!/^[A-Za-z0-9.-]+(?::[0-9]+)?$/.test(host || "")) return null;
	return `https://${host}`;
}

// Replace the GitHub releases default with the mirror origin. The env override
// in install.sh still wins, because only the ${VAR:-default} text changes.
export function rewriteInstallScript(source, host) {
	const base = mirrorBase(host);
	if (!base) return source;
	return source.split(DEFAULT_RELEASE_BASE).join(base).split(DEFAULT_MIRROR_ORIGIN).join(base);
}

function hostAllowed(hostname) {
	if (hostname === "github.com" || hostname === "api.github.com") return true;
	return hostname === "githubusercontent.com" || hostname.endsWith(".githubusercontent.com");
}

// Follow redirects only onto GitHub and its release CDN, so this worker cannot be used as an open proxy.
export async function fetchAllowed(url, accept) {
	let current = url;
	for (let hop = 0; hop < 5; hop++) {
		const parsed = new URL(current);
		if (parsed.protocol !== "https:" || !hostAllowed(parsed.hostname)) {
			throw new Error("upstream host not allowed");
		}
		const res = await fetch(current, {
			redirect: "manual",
			headers: {
				"User-Agent": UA,
				Accept: accept,
			},
		});
		if (res.status < 300 || res.status >= 400) return res;
		const loc = res.headers.get("Location");
		if (!loc) return res;
		current = new URL(loc, current).toString();
	}
	throw new Error("too many redirects");
}

// Read the version from GitHub's /releases/latest redirect, e.g. .../releases/tag/v1.19.33.
export function tagFromReleaseLocation(location) {
	if (!location) return null;
	let pathname;
	try {
		pathname = new URL(location, "https://github.com").pathname;
	} catch {
		return null;
	}
	const matched = pathname.match(/\/releases\/tag\/(v[0-9A-Za-z._-]+)\/?$/);
	return matched ? safeToken(matched[1], MIHOMO_TAG) : null;
}

// Names partyctl selects: plain linux gz first, compatible only as a fallback.
// ABI builds such as mihomo-linux-amd64-v1-<tag>.gz are omitted on purpose.
export function mihomoAssetNames(tag) {
	if (!safeToken(tag, MIHOMO_TAG)) return [];
	const arches = ["linux-amd64", "linux-arm64"];
	const names = arches.map((arch) => `mihomo-${arch}-${tag}.gz`);
	for (const arch of arches) names.push(`mihomo-${arch}-compatible-${tag}.gz`);
	return names;
}

export function mihomoReleaseDocument(tag, host) {
	const base = mirrorBase(host) || "";
	return {
		tag_name: tag,
		assets: mihomoAssetNames(tag).map((name) => ({
			name,
			browser_download_url: `${base}/mihomo/download/${tag}/${name}`,
		})),
	};
}

// Point mihomo asset URLs at this mirror. Other fields are left unchanged.
export function rewriteMihomoRelease(jsonText, host) {
	const base = mirrorBase(host);
	if (!base) return jsonText;
	let data;
	try {
		data = JSON.parse(jsonText);
	} catch {
		return jsonText;
	}
	const prefix = `${MIHOMO_DOWNLOAD}/`;
	if (Array.isArray(data.assets)) {
		for (const asset of data.assets) {
			const raw = asset && asset.browser_download_url;
			if (typeof raw !== "string" || !raw.startsWith(prefix)) continue;
			const rest = raw.slice(prefix.length);
			const slash = rest.indexOf("/");
			if (slash <= 0) continue;
			const tag = safeToken(rest.slice(0, slash), MIHOMO_TAG);
			const file = safeToken(rest.slice(slash + 1), ASSET_NAME);
			if (!tag || !file) continue;
			asset.browser_download_url = `${base}/mihomo/download/${tag}/${file}`;
		}
	}
	return JSON.stringify(data);
}

export function routeRequest(pathname) {
	if (pathname === "/" || pathname === "/install.sh") {
		return { kind: "install" };
	}

	const latest = pathname.match(/^\/latest\/download\/([^/]+)$/);
	if (latest) {
		const file = safeToken(latest[1], /^[A-Za-z0-9._-]+$/);
		if (!file) return { kind: "bad" };
		return {
			kind: "asset",
			upstream: `${DEFAULT_RELEASE_BASE}/latest/download/${file}`,
			cacheControl: "public, max-age=300",
		};
	}

	const tagged = pathname.match(/^\/download\/([^/]+)\/([^/]+)$/);
	if (tagged) {
		const tag = safeToken(tagged[1], /^v[0-9A-Za-z._-]+$/);
		const file = safeToken(tagged[2], /^[A-Za-z0-9._-]+$/);
		if (!tag || !file) return { kind: "bad" };
		return {
			kind: "asset",
			upstream: `${DEFAULT_RELEASE_BASE}/download/${tag}/${file}`,
			cacheControl: "public, max-age=31536000, immutable",
		};
	}

	if (pathname === "/mihomo/releases/latest") {
		return { kind: "mihomo-release", tag: "", cacheControl: "public, max-age=300" };
	}

	const mihomoTag = pathname.match(/^\/mihomo\/releases\/tags\/([^/]+)$/);
	if (mihomoTag) {
		const tag = safeToken(mihomoTag[1], MIHOMO_TAG);
		if (!tag) return { kind: "bad" };
		return { kind: "mihomo-release", tag, cacheControl: "public, max-age=3600" };
	}

	const mihomoFile = pathname.match(/^\/mihomo\/download\/([^/]+)\/([^/]+)$/);
	if (mihomoFile) {
		const tag = safeToken(mihomoFile[1], MIHOMO_TAG);
		const file = safeToken(mihomoFile[2], ASSET_NAME);
		if (!tag || !file) return { kind: "bad" };
		return {
			kind: "asset",
			upstream: `${MIHOMO_DOWNLOAD}/${tag}/${file}`,
			cacheControl: "public, max-age=86400",
			allowed: true,
		};
	}

	const geox = pathname.match(/^\/geox\/download\/([^/]+)\/([^/]+)$/);
	if (geox) {
		const tag = safeToken(geox[1], GEOX_TAG);
		const file = safeToken(geox[2], ASSET_NAME);
		if (!tag || !file) return { kind: "bad" };
		if (!GEOX_FILES.has(file)) return { kind: "notfound" };
		const cacheControl = tag === "latest" ? "public, max-age=300" : "public, max-age=86400";
		return {
			kind: "asset",
			upstream: `${GEOX_DOWNLOAD}/${tag}/${file}`,
			cacheControl,
			allowed: true,
		};
	}

	return { kind: "notfound" };
}

async function fetchUpstream(url, accept) {
	return fetch(url, {
		redirect: "follow",
		headers: {
			"User-Agent": UA,
			Accept: accept,
		},
	});
}

async function serveInstall(request) {
	const host = new URL(request.url).host;
	let upstream;
	try {
		upstream = await fetchUpstream(RELEASE_INSTALL_URL, "text/plain");
		if (!upstream.ok) {
			upstream = await fetchUpstream(RAW_INSTALL_URL, "text/plain");
		}
	} catch {
		return new Response("install.sh unavailable\n", { status: 502 });
	}
	if (!upstream.ok) {
		return new Response("install.sh unavailable\n", { status: 502 });
	}

	const source = await upstream.text();
	const body = rewriteInstallScript(source, host);
	const headers = {
		"Content-Type": "text/x-shellscript; charset=utf-8",
		"Cache-Control": "public, max-age=300",
	};
	if (request.method === "HEAD") {
		headers["Content-Length"] = String(new TextEncoder().encode(body).length);
		return new Response(null, { status: 200, headers });
	}
	return new Response(body, { status: 200, headers });
}

function noStore(extra) {
	return { "Cache-Control": "no-store", ...extra };
}

// Pass the upstream status and body through. Error responses must not be cached.
async function upstreamError(res) {
	let text = "";
	try {
		text = await res.text();
	} catch {
		text = "";
	}
	const type = res.headers.get("Content-Type") || "text/plain; charset=utf-8";
	return new Response(text || `upstream ${res.status}\n`, {
		status: res.status || 502,
		headers: noStore({ "Content-Type": type }),
	});
}

async function fetchGitHub(url, method = "GET") {
	const parsed = new URL(url);
	if (parsed.protocol !== "https:" || !hostAllowed(parsed.hostname)) {
		throw new Error("upstream host not allowed");
	}
	return fetch(url, {
		method,
		redirect: "manual",
		headers: {
			"User-Agent": UA,
			Accept: "text/html,application/xhtml+xml",
		},
	});
}

async function serveMihomoRelease(request, route) {
	let tag = route.tag;
	try {
		if (!tag) {
			const res = await fetchGitHub(MIHOMO_LATEST_PAGE, "GET");
			if (res.status < 300 || res.status >= 400) return upstreamError(res);
			tag = tagFromReleaseLocation(res.headers.get("Location"));
			if (!tag) {
				const loc = res.headers.get("Location") || "";
				return new Response(`cannot parse mihomo version from ${loc}\n`, {
					status: 502,
					headers: noStore({ "Content-Type": "text/plain; charset=utf-8" }),
				});
			}
		} else {
			const head = await fetchGitHub(`${MIHOMO_TAG_PAGE}${tag}`, "HEAD");
			if (head.status >= 400) {
				// HEAD often has an empty body. GET so the client can see GitHub's error.
				const get = await fetchGitHub(`${MIHOMO_TAG_PAGE}${tag}`, "GET");
				return upstreamError(get.status ? get : head);
			}
		}
	} catch (err) {
		const message = err && err.message ? err.message : "upstream unavailable";
		return new Response(`${message}\n`, {
			status: 502,
			headers: noStore({ "Content-Type": "text/plain; charset=utf-8" }),
		});
	}

	const body = JSON.stringify(mihomoReleaseDocument(tag, new URL(request.url).host));
	const headers = {
		"Content-Type": "application/json; charset=utf-8",
		"Cache-Control": route.cacheControl,
	};
	if (request.method === "HEAD") {
		headers["Content-Length"] = String(new TextEncoder().encode(body).length);
		return new Response(null, { status: 200, headers });
	}
	return new Response(body, { status: 200, headers });
}

async function serveAsset(request, route) {
	let upstream;
	try {
		const accept = "application/octet-stream";
		upstream = route.allowed
			? await fetchAllowed(route.upstream, accept)
			: await fetchUpstream(route.upstream, accept);
	} catch (err) {
		const message = err && err.message ? err.message : "upstream unavailable";
		return new Response(`${message}\n`, {
			status: 502,
			headers: noStore({ "Content-Type": "text/plain; charset=utf-8" }),
		});
	}
	if (!upstream.ok || !upstream.body) {
		return upstreamError(upstream);
	}

	const headers = new Headers();
	const type = upstream.headers.get("Content-Type");
	headers.set("Content-Type", type || "application/octet-stream");
	const len = upstream.headers.get("Content-Length");
	if (len) headers.set("Content-Length", len);
	const disp = upstream.headers.get("Content-Disposition");
	if (disp) headers.set("Content-Disposition", disp);
	headers.set("Cache-Control", route.cacheControl);

	if (request.method === "HEAD") {
		return new Response(null, { status: 200, headers });
	}
	return new Response(upstream.body, { status: 200, headers });
}

export default {
	async fetch(request) {
		if (request.method !== "GET" && request.method !== "HEAD") {
			return new Response("method not allowed\n", {
				status: 405,
				headers: { Allow: "GET, HEAD" },
			});
		}

		const route = routeRequest(new URL(request.url).pathname);
		if (route.kind === "install") return serveInstall(request);
		if (route.kind === "mihomo-release") return serveMihomoRelease(request, route);
		if (route.kind === "asset") return serveAsset(request, route);
		if (route.kind === "bad") return new Response("bad request\n", { status: 400 });
		return new Response("not found\n", { status: 404 });
	},
};
