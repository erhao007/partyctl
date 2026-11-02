const GITHUB_REPO = "erhao007/partyctl";

// Default release base baked into install.sh. The mirror rewrites this to https://<host>.
export const DEFAULT_RELEASE_BASE = `https://github.com/${GITHUB_REPO}/releases`;

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
	return source.split(DEFAULT_RELEASE_BASE).join(base);
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

async function serveAsset(request, route) {
	let upstream;
	try {
		upstream = await fetchUpstream(route.upstream, "application/octet-stream");
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
		if (route.kind === "asset") return serveAsset(request, route);
		if (route.kind === "bad") return new Response("bad request\n", { status: 400 });
		return new Response("not found\n", { status: 404 });
	},
};
