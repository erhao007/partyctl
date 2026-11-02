# partyctl

基于 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）的 Ubuntu 命令行轻量封装，灵感来自 [Clash Party](https://github.com/mihomo-party-org/clash-party)。提供 CLI 与嵌入式中文 Web 管理界面。

## 依赖

- Go 1.22+（构建时；使用 `net/http` 方法路由）
- Linux amd64 / arm64
- 可选：`systemctl --user`、`gsettings`（GNOME）、`setcap`（TUN）

## 安装

一条命令即可。脚本会装好 `partyctl`、在还没有内核时执行 `partyctl core install`，并启动 Web 管理界面（尽量设为开机或登录自启）。没有订阅时页面也能打开，登录后在「订阅」里添加并启用，再点「启动」运行内核。

```bash
# 推荐：root 安装到 /usr/local/bin，并用 systemd 系统服务开机拉起 Web
curl -fsSL https://get.251819788.xyz/install.sh | sh

# 非 root：装到 ~/.local/bin。有用户级 systemd 时登录自启，否则后台运行
curl -fsSL https://get.251819788.xyz/install.sh | sh -s -- --user

# 只装程序，不装内核、不启动 Web
curl -fsSL https://get.251819788.xyz/install.sh | sh -s -- --no-start
curl -fsSL https://get.251819788.xyz/install.sh | PARTYCTL_NO_START=1 sh

# 备用：GitHub raw
curl -fsSL https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh | sh

# 或从源码构建后自行拉起 Web
cd partyctl
go build -o partyctl .
sudo install -m 755 partyctl /usr/local/bin/partyctl
partyctl core install
partyctl web enable
```

结束时会打印 Web 地址和登录令牌。重复运行是升级：保留 `~/.config/partyctl`，已能运行的内核不会重新下载，正在运行的 Web 会重启到新程序。更新内核用 `partyctl core install`（不要加 `--if-missing`）。

安装或查看内核：

```bash
partyctl core install              # 经镜像下载最新内核，失败再试 GitHub
partyctl core install --if-missing # 已有可运行内核时跳过
partyctl core install --mirror off # 只从 GitHub 下载
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
| `~/.config/partyctl/web.log` | 没有 systemd 时，后台 Web 的日志 |
| `~/.config/systemd/user/partyctl.service` | 用户级 systemd 单元（内核） |
| `~/.config/systemd/user/partyctl-web.service` | 用户级 Web 服务（登录自启） |
| `/etc/systemd/system/partyctl-web.service` | root 安装时的系统级 Web 服务（开机自启） |

`config.yaml` 中与 Web 相关的字段示例：

```yaml
web:
  enabled: false          # true 时 partyctl start 会尝试后台拉起 Web
  listen: 127.0.0.1:9800  # 默认仅本机
  token: ""               # 为空则复用 secret
```

## 快速开始

安装脚本跑完后，用它打印的地址和令牌打开 Web。还没有订阅也可以登录。在「订阅」页添加并启用，再点右上角「启动」。

也可以继续用命令行：

```bash
partyctl core install
partyctl sub add mysub 'https://example.com/clash.yaml'
partyctl sub use mysub
partyctl start --detach          # 自动 generate；Web 由 partyctl web enable 单独托管
partyctl web url
partyctl web token
# 浏览器打开 http://127.0.0.1:9800/ 。远程：ssh -L 9800:127.0.0.1:9800 user@host
```

Web 的自启方式：

| 环境 | 行为 |
|------|------|
| root，且本机有 systemd | `/etc/systemd/system/partyctl-web.service`，开机自启 |
| 普通用户，且 `systemctl --user` 可用 | `~/.config/systemd/user/partyctl-web.service`，登录自启 |
| 没有 systemd，或写服务失败 | 后台运行（`--detach`），日志在 `~/.config/partyctl/web.log`，开机后需再执行 `partyctl web enable` |

```bash
partyctl web enable                 # 按上面的规则安装并启动（已在跑则重启）
partyctl web lan on                 # 监听 0.0.0.0 并重启，供局域网访问
partyctl web lan off                # 恢复只监听 127.0.0.1
partyctl web --listen 127.0.0.1:9800  # 前台运行，不安装服务
```

`web lan on` 会打印本机各网卡的访问地址，以及 `partyctl web token`。只在可信网络上开启。如果装了 ufw 并且可能拦截，会提示 `sudo ufw allow <端口>/tcp`，不会自动改防火墙。

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
- 功能：状态与启停、模式、订阅（添加、启用、更新、删除，含 subscription-userinfo 流量/到期解析）、覆盖编辑、节点选择与测速、实时流量与日志（SSE 代理内核）
- 没有当前订阅时页面仍可打开。第一次添加订阅会自动启用；已有订阅时添加不会擅自切换。内核未安装或未选订阅时，点「启动」会提示原因

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
| `core install [--version] [--if-missing] [--mirror]` / `core version` | 经镜像下载安装（失败回退 GitHub）/ 已有则跳过 / 查看内核 |
| `sub add/update/use/list/rm` | 订阅管理 |
| `override add/list/rm` | 覆盖管理 |
| `generate` | 生成配置并校验（`start` 也会自动执行） |
| `start/stop/restart/status` | 生命周期；`status` 汇总内核、Web 与路径；`start --with-web` 会再后台拉起一份 Web |
| `web [--listen]` / `web enable` / `web lan on\|off` / `web url` / `web token` | 前台启动 / 安装并自启 / 局域网访问开关 / 打印地址 / 打印登录令牌 |
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

一键安装（`latest` 会跟随 `/latest/download/` 的重定向解析出版本标签）。装完会继续安装内核并启动 Web；`--no-start` 或 `PARTYCTL_NO_START=1` 可跳过这两步：

```bash
curl -fsSL https://get.251819788.xyz/install.sh | sh
# 备用
curl -fsSL https://raw.githubusercontent.com/erhao007/partyctl/main/install.sh | sh
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

`cloudflare/` 是一个 Worker，已配置自定义域名 `get.251819788.xyz`（`workers.dev` 仍开启）。`GET /` 与 `GET /install.sh` 拉取最新 Release 里的 `install.sh`（没有则用 main 上的 raw 文件），并把默认 `PARTYCTL_BASE_URL` 和 `PARTYCTL_MIRROR` 改写为 `https://<请求主机>`。`GET /latest/download/<file>` 与 `GET /download/<tag>/<file>` 流式代理 partyctl 的 GitHub Release 资源。

内核和 Geo 文件也走同一个域名，只允许这两个上游，不能拿来代理任意地址：

| 路径 | 上游 |
|------|------|
| `GET /mihomo/releases/latest` | 跟随 `github.com/MetaCubeX/mihomo/releases/latest` 的重定向得到版本号，再按 `mihomo-linux-<arch>-<tag>.gz` 拼资产列表。不访问 `api.github.com` |
| `GET /mihomo/releases/tags/<v标签>` | 确认该 tag 页面存在后，按同样规则拼资产列表 |
| `GET /mihomo/download/<v标签>/<文件>` | `github.com/MetaCubeX/mihomo/releases/download/...` |
| `GET /geox/download/<latest 或 v标签>/<文件>` | `MetaCubeX/meta-rules-dat` 里固定的 `geoip.dat`、`geosite.dat`、`geoip.metadb`、`GeoLite2-ASN.mmdb` |

`partyctl core install` 默认请求 `https://get.251819788.xyz`（可用 `PARTYCTL_MIRROR` 或 `--mirror` 覆盖，`off` 表示只走 GitHub）。镜像失败会再试 GitHub。生成配置时，如果订阅没有自己的 `geox-url`，会写成上面的 `/geox/download/latest/...`。重定向只跟随 `github.com`、`api.github.com` 和 `*.githubusercontent.com`。

修改 `cloudflare/worker.js` 之后要在 `cloudflare/` 里重新执行 `npx wrangler deploy`，已发布的二进制要等下一个版本才会改走这些路径。

部署时把 API 令牌放在环境变量 `CLOUDFLARE_API_TOKEN` 里，不要写入仓库：

```bash
cd cloudflare
CLOUDFLARE_API_TOKEN=你的令牌 npx wrangler deploy
```

```bash
curl -fsSL https://get.251819788.xyz/install.sh | sh
```

## 许可

MVP 示例代码，按需使用。mihomo / Clash Party 遵循各自上游许可。
