// Package mirror 解析内核与 Geo 文件使用的下载镜像。
// 默认是 Cloudflare Worker https://get.251819788.xyz；PARTYCTL_MIRROR=off 时关闭镜像。
package mirror

import (
	"os"
	"strings"
)

// DefaultBase 是已部署的 partyctl 下载镜像。
const DefaultBase = "https://get.251819788.xyz"

// Env 是覆盖默认镜像的环境变量名。
const Env = "PARTYCTL_MIRROR"

// Base 返回当前生效的镜像原点。关闭镜像时返回空字符串。
func Base() string {
	v, ok := os.LookupEnv(Env)
	if !ok {
		return DefaultBase
	}
	return Normalize(v)
}

// Normalize 把用户输入整理成镜像原点。off、github、direct、空字符串表示不用镜像。
func Normalize(v string) string {
	v = strings.TrimRight(strings.TrimSpace(v), "/")
	switch strings.ToLower(v) {
	case "", "off", "github", "direct", "none":
		return ""
	default:
		return v
	}
}

// GeoxURLs 是 mihomo 在配置里没有 geox-url 时使用的四个规则文件地址。
func GeoxURLs(base string) map[string]string {
	if base == "" {
		return nil
	}
	return map[string]string{
		"geoip":   base + "/geox/download/latest/geoip.dat",
		"geosite": base + "/geox/download/latest/geosite.dat",
		"mmdb":    base + "/geox/download/latest/geoip.metadb",
		"asn":     base + "/geox/download/latest/GeoLite2-ASN.mmdb",
	}
}
