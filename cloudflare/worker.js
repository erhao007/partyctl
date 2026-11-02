const GITHUB_REPO = "erhao007/partyctl";

// Default release base baked into install.sh. The mirror rewrites this to https://<host>.
export const DEFAULT_RELEASE_BASE = `https://github.com/${GITHUB_REPO}/releases`;

// Default kernel/geox mirror baked into install.sh. Rewritten to the request host.
export const DEFAULT_MIRROR_ORIGIN = "https://get.251819788.xyz";

export const MIHOMO_API = "https://api.github.com/repos/MetaCubeX/mihomo/releases";
export const MIHOMO_DOWNLOAD = "https://github.com/MetaCubeX/mihomo/releases/download";
export const GEOX_DOWNLOAD = "https://github.com/MetaCubeX/meta-rules-dat/releases/download";

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
		return {
			kind: "mihomo-api",
			upstream: `${MIHOMO_API}/latest`,
			cacheControl: "public, max-age=300",
		};
	}

	const mihomoTag = pathname.match(/^\/mihomo\/releases\/tags\/([^/]+)$/);
	if (mihomoTag) {
		const tag = safeToken(mihomoTag[1], MIHOMO_TAG);
		if (!tag) return { kind: "bad" };
		return {
			kind: "mihomo-api",
			upstream: `${MIHOMO_API}/tags/${tag}`,
			cacheControl: "public, max-age=3600",
		};
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

async function serveMihomoAPI(request, route) {
	let upstream;
	try {
		upstream = await fetchAllowed(route.upstream, "application/vnd.github+json");
	} catch {
		return new Response("upstream unavailable\n", { status: 502 });
	}
	if (!upstream.ok) {
		return new Response("mihomo release unavailable\n", { status: upstream.status || 502 });
	}
	const source = await upstream.text();
	const body = rewriteMihomoRelease(source, new URL(request.url).host);
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
	} catch {
		return new Response("upstream unavailable\n", { status: 502 });
	}
	if (!upstream.ok || !upstream.body) {
		return new Response("asset unavailable\n", { status: upstream.status || 502 });
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
		if (route.kind === "mihomo-api") return serveMihomoAPI(request, route);
		if (route.kind === "asset") return serveAsset(request, route);
		if (route.kind === "bad") return new Response("bad request\n", { status: 400 });
		return new Response("not found\n", { status: 404 });
	},
};
