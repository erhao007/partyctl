// 把按进程代理需要的 tproxy 端口、routing-mark 和 DNS 写进运行配置。
// 只在功能开启时生效，并且不改 tun 开关。
package procproxy

import (
	"fmt"
	"strconv"
	"strings"
)

// ApplyRuntime 在生成 mihomo 配置的最后补上透明代理字段。未开启时什么都不改。
func ApplyRuntime(profile map[string]any) {
	mu.Lock()
	defer mu.Unlock()
	st := load()
	if !st.Enabled {
		return
	}
	profile["tproxy-port"] = TProxyPort
	mark := asInt(profile["routing-mark"])
	if mark == 0 || mark == FwMark {
		mark = RouteMark
		profile["routing-mark"] = mark
	}
	port := ensureDNS(profile)
	st.RouteMark = mark
	st.DNSPort = port
	_ = save(st)
}

// ensureDNS 保证有一个不会和 53 端口自环的 DNS 监听。已有的 nameserver 保持不变。
func ensureDNS(profile map[string]any) int {
	raw, _ := profile["dns"].(map[string]any)
	if raw == nil {
		raw = map[string]any{
			"enable":        true,
			"listen":        fmt.Sprintf("127.0.0.1:%d", DNSPort),
			"ipv6":          true,
			"enhanced-mode": "fake-ip",
			"fake-ip-range": "198.18.0.1/16",
			"nameserver": []any{
				"https://dns.alidns.com/dns-query",
				"https://doh.pub/dns-query",
			},
			"proxy-server-nameserver": []any{
				"tls://223.5.5.5",
				"tls://1.1.1.1",
			},
		}
		profile["dns"] = raw
		return DNSPort
	}
	raw["enable"] = true
	port := listenPort(raw["listen"])
	if port == 0 || port == 53 {
		raw["listen"] = fmt.Sprintf("127.0.0.1:%d", DNSPort)
		port = DNSPort
	}
	profile["dns"] = raw
	return port
}

func listenPort(v any) int {
	if v == nil {
		return 0
	}
	s := fmt.Sprint(v)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}
