#!/bin/sh
# Install partyctl from a GitHub release, or from a mirror that uses the same path layout.
# POSIX sh. After the binary is in place, installs mihomo if missing and starts the Web UI
# unless --no-start / PARTYCTL_NO_START is set.
# Version and base URL come from the environment:
#   PARTYCTL_VERSION   default: latest
#   PARTYCTL_BASE_URL  default: https://github.com/erhao007/partyctl/releases
set -eu

PARTYCTL_VERSION="${PARTYCTL_VERSION:-latest}"
PARTYCTL_BASE_URL="${PARTYCTL_BASE_URL:-https://github.com/erhao007/partyctl/releases}"

user_install=0
no_start=0
case "$(printf '%s' "${PARTYCTL_NO_START:-}" | tr '[:upper:]' '[:lower:]')" in
1 | true | yes | on) no_start=1 ;;
esac

usage() {
	cat <<'EOF'
Usage: install.sh [--user] [--no-start]

  --user       安装到 ~/.local/bin（不使用 sudo）
               省略时安装到 /usr/local/bin，需要时使用 sudo
  --no-start   只安装 partyctl 程序，不安装内核、不启动 Web

默认会在装好程序后：
  1. 若本机还没有可用的 mihomo，执行 partyctl core install
  2. 启动 Web 管理界面，并尽量设为开机或登录自启
重复运行会保留已有配置；内核已存在则不重新下载；正在运行的 Web 会重启到新版本。

Environment:
  PARTYCTL_VERSION    release tag, or "latest" (default: latest)
  PARTYCTL_BASE_URL   release base URL
                      (default: https://github.com/erhao007/partyctl/releases)
  PARTYCTL_NO_START   设为 1/true/yes/on 时等同于 --no-start
                      要加在执行脚本的 sh 上，例如：
                      curl -fsSL .../install.sh | PARTYCTL_NO_START=1 sh
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

# print_manual_next 在 --no-start 时告诉用户还要自己做的步骤。
print_manual_next() {
	cmd=$1
	cat <<EOF

已跳过内核安装和 Web 启动（--no-start / PARTYCTL_NO_START）。

下一步:
  ${cmd} core install
  ${cmd} sub add mysub 'https://example.com/clash.yaml'
  ${cmd} sub use mysub
  ${cmd} web enable
  或: ${cmd} start --detach --with-web

Web 默认地址: http://127.0.0.1:9800
登录令牌: ${cmd} web token
远程: ssh -L 9800:127.0.0.1:9800 user@host
EOF
}

# print_ready_summary 在安装结束时打印 Web 地址、令牌和远程访问方式。
print_ready_summary() {
	setup_warn=$1
	shown=$2
	dest=$3
	cmd=$4
	web_url=$5
	web_token=$6
	web_port=9800
	case "$web_url" in
	*:*)
		web_port=${web_url##*:}
		web_port=${web_port%%/*}
		;;
	esac
	if [ -z "$web_url" ]; then
		web_url="http://127.0.0.1:9800"
	fi
	echo
	if [ "$setup_warn" -eq 0 ]; then
		echo "partyctl 已就绪"
	else
		echo "partyctl 已安装（有步骤未完成，见上面的警告）"
	fi
	echo "程序: ${dest} (${shown})"
	echo "Web:  ${web_url}"
	if [ -n "$web_token" ]; then
		echo "登录令牌: ${web_token}"
	else
		echo "登录令牌: 暂时读不到，请稍后执行 ${cmd} web token"
	fi
	echo "以后查看令牌: ${cmd} web token"
	cat <<EOF

在这台机器的浏览器打开上面的地址，用令牌登录。
没有订阅也可以打开。登录后到「订阅」添加并启用，再点「启动」运行内核。

默认只监听本机。从别的电脑访问请做 SSH 转发:
  ssh -L ${web_port}:127.0.0.1:${web_port} user@这台机器
然后在你自己的浏览器打开 http://127.0.0.1:${web_port}

若要让局域网直接访问，把 Web 监听改成 0.0.0.0:${web_port} 后重新执行:
  ${cmd} web enable
（只在可信网络上这样做。）
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--user)
		user_install=1
		;;
	--no-start)
		no_start=1
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
echo "已安装 partyctl ${shown} -> ${dest}"
if [ "$(id -u)" -eq 0 ] && [ "$user_install" -eq 1 ]; then
	echo "当前是 root，--user 仍安装到 ${dest_dir}。省略 --user 则安装到 /usr/local/bin。"
fi
partyctl_cmd="partyctl"
case ":${PATH}:" in
*":${dest_dir}:"*) ;;
*)
	partyctl_cmd="$dest"
	echo "请将 ${dest_dir} 加入 PATH:"
	echo "  export PATH=\"${dest_dir}:\$PATH\""
	;;
esac

if [ "$no_start" -eq 1 ]; then
	print_manual_next "$partyctl_cmd"
	exit 0
fi

setup_warn=0
echo "正在检查 mihomo 内核…"
if ! "$dest" core install --if-missing; then
	echo "警告: 内核没有安装成功。partyctl 程序本身已经装好，已有配置不会被改动。" >&2
	echo "请稍后重试: ${partyctl_cmd} core install" >&2
	echo "常见原因是暂时访问不了 GitHub。Web 管理界面仍会继续启动，你可以先打开页面。" >&2
	setup_warn=1
fi

echo "正在启动 Web 管理界面…"
if ! "$dest" web enable; then
	echo "警告: Web 管理界面没有启动成功。程序已安装，可稍后执行: ${partyctl_cmd} web enable" >&2
	setup_warn=1
fi

web_url=$("$dest" web url 2>/dev/null) || web_url=""
web_token=$("$dest" web token 2>/dev/null) || web_token=""
print_ready_summary "$setup_warn" "$shown" "$dest" "$partyctl_cmd" "$web_url" "$web_token"
