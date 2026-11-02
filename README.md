# partyctl

基于 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）的 Ubuntu 命令行轻量封装，灵感来自 [Clash Party](https://github.com/mihomo-party-org/clash-party)。提供 CLI 与嵌入式中文 Web 管理界面。

## 依赖

- Go 1.22+（构建时；使用 `net/http` 方法路由）
- Linux amd64 / arm64
- 可选：`systemctl --user`、`gsettings`（GNOME）、`setcap`（TUN）

## 安装

```bash
# 从源码构建
cd partyctl
go build -o partyctl .
sudo install -m 755 partyctl /usr/local/bin/partyctl

# 或使用预编译二进制
install -m 755 partyctl-linux-amd64 ~/.local/bin/partyctl
```

安装内核：

```bash
partyctl core install
partyctl core version
```

## 目录布局

| 路径 | 说明 |
|------|------|
| `~/.config/partyctl/config.yaml` | 设置：mixed-port、controller、secret、当前配置、TUN、web、overrides |
| `~/.config/partyctl/profiles/<name>.yaml` | 订阅/本地配置内容 |
| `~/.config/partyctl/profiles.yaml` | 配置元数据（url、updated、subscription-userinfo） |
| `~/.config/partyctl/override/*.yaml` | 覆盖片段 |
| `~/.config/partyctl/work/config.yaml` | 生成后的运行配置 |
| `~/.local/share/partyctl/mihomo` | 内核二进制 |
| `~/.config/systemd/user/partyctl.service` | 用户级 systemd 单元 |

`config.yaml` 中与 Web 相关的字段示例：

```yaml
web:
  enabled: false          # true 时 partyctl start 会尝试后台拉起 Web
  listen: 127.0.0.1:9800  # 默认仅本机
  token: ""               # 为空则复用 secret
```

## 快速开始

```bash
partyctl core install
partyctl sub add mysub 'https://example.com/clash.yaml'
partyctl sub use mysub
partyctl generate
partyctl start --detach --with-web
# 浏览器打开 http://127.0.0.1:9800/ ，用 secret（或 web.token）登录
```

也可单独启动 Web：

```bash
partyctl web --listen 127.0.0.1:9800
```

### CLI 其余常用命令

```bash
partyctl mode rule
partyctl node list
partyctl node select PROXY '节点名'
partyctl override add /tmp/ov.yaml
partyctl proxy env
partyctl tun on
```

## Web 界面

- 纯 HTML/JS/CSS，经 `go:embed` 打进二进制，无前端构建步骤
- 默认绑定 `127.0.0.1`；绑定非回环地址时会在 stderr 打印警告
- 登录页 + `Authorization: Bearer <token>` / Cookie；token 默认等于控制器 `secret`
- 功能：状态与启停、模式、订阅（含 subscription-userinfo 流量/到期解析）、覆盖编辑、节点选择与测速、实时流量与日志（SSE 代理内核）

主要 API（均需鉴权，除 `POST /api/login`）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/status` | 运行状态 |
| POST | `/api/core/{start\|stop\|restart}` | 控制内核 |
| GET/PUT | `/api/settings` | 读/写设置并 regenerate |
| GET/POST/DELETE… | `/api/subs` | 订阅管理 |
| GET/PUT/DELETE | `/api/overrides…` | 覆盖管理 |
| GET/PUT | `/api/proxies…` | 节点 |
| GET | `/api/delay/{name}` | 延迟 |
| GET | `/api/traffic` `/api/logs` | SSE |

## 命令一览

| 命令 | 说明 |
|------|------|
| `core install [--version]` / `core version` | 下载安装 / 查看内核 |
| `sub add/update/use/list/rm` | 订阅管理 |
| `override add/list/rm` | 覆盖管理 |
| `generate` | 生成配置并校验 |
| `start/stop/restart/status` | 生命周期；`start --with-web` 可同启 Web |
| `web [--listen]` | 嵌入式 Web UI |
| `mode` / `node` / `log` / `proxy` / `tun` | 控制与系统代理 |
| `version` | 显示 partyctl 版本（发布包为 git 标签，本地构建为 `dev`） |

## 本地最小配置示例

```yaml
# ~/.config/partyctl/profiles/local.yaml
proxies:
  - name: DIRECT-NODE
    type: direct
proxy-groups:
  - name: PROXY
    type: select
    proxies: [DIRECT, DIRECT-NODE]
rules:
  - MATCH,PROXY
```

## 开发

```bash
go test ./...
go vet ./...
go build -o partyctl .
```

## 发布与下载

推送 `v*` 标签后，GitHub Actions 会构建 linux/amd64 与 linux/arm64 静态二进制（`-ldflags` 注入标签，`partyctl version` 即该标签），打成含 README 的 tar.gz，并额外附上稳定文件名 `partyctl-linux-<arch>.tar.gz`（供 `latest/download` 使用）、`SHA256SUMS` 和 `install.sh`，发布到 [GitHub Releases](https://github.com/erhao007/partyctl/releases)。

```bash
git tag v0.1.0 && git push origin v0.1.0
```

一键安装（`latest` 会跟随 `/latest/download/` 的重定向解析出版本标签）：

```bash
curl -fsSL https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh | sh -s -- --user
```

也可手动下载对应架构的压缩包和 `SHA256SUMS`，校验后安装：

```bash
sha256sum -c SHA256SUMS
tar -xzf partyctl-v0.1.0-linux-amd64.tar.gz
sudo install -m 755 partyctl-v0.1.0-linux-amd64/partyctl /usr/local/bin/partyctl
partyctl version
```

arm64 把文件名中的 `amd64` 换成 `arm64`。`PARTYCTL_VERSION` 可指定标签（默认 `latest`），`PARTYCTL_BASE_URL` 默认为 `https://github.com/erhao007/partyctl/releases`。

## Cloudflare 镜像

`cloudflare/` 是一个 Worker：`GET /` 与 `GET /install.sh` 拉取最新 Release 里的 `install.sh`（没有则用 main 上的 raw 文件），并把默认 `PARTYCTL_BASE_URL` 改写为 `https://<请求主机>`。`GET /latest/download/<file>` 与 `GET /download/<tag>/<file>` 流式代理 GitHub Release 资源（跟随重定向），并带上缓存头。

```bash
cd cloudflare
# 把 wrangler.toml 里的 partyctl.example.com 和 example.com 换成你的域名
npx wrangler deploy
```

部署后在 Cloudflare 控制台给这个 Worker 绑定自定义域名，然后：

```bash
curl -fsSL https://你的域名/install.sh | sh
```

## 许可

MVP 示例代码，按需使用。mihomo / Clash Party 遵循各自上游许可。
