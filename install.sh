#!/bin/sh
# Install partyctl from a GitHub release, or from a mirror that uses the same path layout.
# POSIX sh. Version and base URL come from the environment:
#   PARTYCTL_VERSION   default: latest
#   PARTYCTL_BASE_URL  default: https://github.com/erhao007/partyctl/releases
set -eu

PARTYCTL_VERSION="${PARTYCTL_VERSION:-latest}"
PARTYCTL_BASE_URL="${PARTYCTL_BASE_URL:-https://github.com/erhao007/partyctl/releases}"

user_install=0

usage() {
	cat <<'EOF'
Usage: install.sh [--user]

  --user    install to ~/.local/bin (no sudo)
            otherwise install to /usr/local/bin, using sudo when needed

Environment:
  PARTYCTL_VERSION    release tag, or "latest" (default: latest)
  PARTYCTL_BASE_URL   release base URL
                      (default: https://github.com/erhao007/partyctl/releases)
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--user)
		user_install=1
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown argument: $1"
		;;
	esac
	shift
done

if ! command -v curl >/dev/null 2>&1; then
	die "curl is required"
fi
if ! command -v tar >/dev/null 2>&1; then
	die "tar is required"
fi
if ! command -v sha256sum >/dev/null 2>&1; then
	die "sha256sum is required"
fi

os=$(uname -s)
if [ "$os" != "Linux" ]; then
	die "Linux is required (found ${os})"
fi

machine=$(uname -m)
case "$machine" in
x86_64 | amd64)
	arch=amd64
	;;
aarch64 | arm64)
	arch=arm64
	;;
*)
	die "unsupported architecture: ${machine} (need amd64 or arm64)"
	;;
esac

case "$PARTYCTL_VERSION" in
latest) ;;
v[0-9]*)
	case "$PARTYCTL_VERSION" in
	*[!A-Za-z0-9._-]*)
		die "invalid PARTYCTL_VERSION: ${PARTYCTL_VERSION}"
		;;
	esac
	;;
*)
	die "invalid PARTYCTL_VERSION: ${PARTYCTL_VERSION} (use latest or a v* tag)"
	;;
esac

# Trim trailing slashes so path joins stay single-slash.
PARTYCTL_BASE_URL=$(printf '%s' "$PARTYCTL_BASE_URL" | sed 's:/*$::')
if [ -z "$PARTYCTL_BASE_URL" ]; then
	die "PARTYCTL_BASE_URL is empty"
fi

curl_common() {
	curl -fsSL -A partyctl-install "$@"
}

# "latest" is resolved from the redirect of <base>/latest/download/<stable-asset>.
# GitHub sends Location: .../download/<tag>/partyctl-linux-<arch>.tar.gz.
# A mirror that streams the body with no redirect falls through to the stable name.
if [ "$PARTYCTL_VERSION" = "latest" ]; then
	probe="${PARTYCTL_BASE_URL}/latest/download/partyctl-linux-${arch}.tar.gz"
	headers=$(curl -fsI -A partyctl-install "$probe" || true)
	tag=$(printf '%s\n' "$headers" | tr -d '\r' | sed -n 's#^[Ll]ocation: .*/download/\(v[0-9][^/]*\)/.*#\1#p' | head -n 1)
	if [ -n "$tag" ]; then
		PARTYCTL_VERSION=$tag
	fi
fi

if [ "$PARTYCTL_VERSION" = "latest" ]; then
	asset="partyctl-linux-${arch}.tar.gz"
	tarball_url="${PARTYCTL_BASE_URL}/latest/download/${asset}"
	sums_url="${PARTYCTL_BASE_URL}/latest/download/SHA256SUMS"
else
	asset="partyctl-${PARTYCTL_VERSION}-linux-${arch}.tar.gz"
	tarball_url="${PARTYCTL_BASE_URL}/download/${PARTYCTL_VERSION}/${asset}"
	sums_url="${PARTYCTL_BASE_URL}/download/${PARTYCTL_VERSION}/SHA256SUMS"
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT TERM

echo "Downloading ${tarball_url}"
curl_common -o "${tmpdir}/${asset}" "$tarball_url"
curl_common -o "${tmpdir}/SHA256SUMS" "$sums_url"

expected=$(awk -v name="$asset" '$2 == name { print $1; exit }' "${tmpdir}/SHA256SUMS")
if [ -z "$expected" ]; then
	die "SHA256SUMS has no entry for ${asset}"
fi
actual=$(sha256sum "${tmpdir}/${asset}" | awk '{ print $1 }')
if [ "$expected" != "$actual" ]; then
	die "checksum mismatch for ${asset}: expected ${expected}, got ${actual}"
fi
echo "Checksum OK (${asset})"

tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"
bin=$(find "$tmpdir" -type f -name partyctl | head -n 1)
if [ -z "$bin" ]; then
	die "archive does not contain a partyctl binary"
fi

if [ "$user_install" -eq 1 ]; then
	if [ -z "${HOME:-}" ]; then
		die "HOME is not set"
	fi
	dest_dir="${HOME}/.local/bin"
	mkdir -p "$dest_dir"
else
	dest_dir="/usr/local/bin"
	if [ ! -d "$dest_dir" ]; then
		if [ "$(id -u)" -eq 0 ]; then
			mkdir -p "$dest_dir"
		else
			sudo mkdir -p "$dest_dir"
		fi
	fi
fi

dest="${dest_dir}/partyctl"
if [ -w "$dest_dir" ] || [ "$(id -u)" -eq 0 ]; then
	if command -v install >/dev/null 2>&1; then
		install -m 755 "$bin" "$dest"
	else
		cp "$bin" "$dest"
		chmod 755 "$dest"
	fi
else
	if command -v install >/dev/null 2>&1; then
		sudo install -m 755 "$bin" "$dest"
	else
		sudo cp "$bin" "$dest"
		sudo chmod 755 "$dest"
	fi
fi

shown="$PARTYCTL_VERSION"
if bin_version=$("$dest" version 2>/dev/null); then
	if [ -n "$bin_version" ]; then
		shown=$bin_version
	fi
fi
echo "Installed partyctl ${shown} -> ${dest}"
if [ "$user_install" -eq 1 ]; then
	case ":${PATH}:" in
	*":${dest_dir}:"*) ;;
	*)
		echo "Add ${dest_dir} to PATH if needed:"
		echo "  export PATH=\"${dest_dir}:\$PATH\""
		;;
	esac
fi

cat <<'EOF'

Next steps:
  partyctl version
  partyctl core install
  partyctl sub add mysub 'https://example.com/clash.yaml'
  partyctl sub use mysub
  partyctl generate
  partyctl start --detach --with-web
EOF
