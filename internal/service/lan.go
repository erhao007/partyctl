// 本文件把 Web 管理界面切换到局域网监听或收回到本机，并重启已经托管的 Web 进程。
package service

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/partyctl/partyctl/internal/config"
)

// restartWebForLAN 在测试里替换，避免真的拉起当前进程。
var restartWebForLAN = EnableWeb

// WebLAN 开启或关闭局域网访问：改 web.listen，然后按现有方式重启 Web。
func WebLAN(on bool) (string, error) {
	st, err := setWebLAN(on)
	if err != nil {
		return "", err
	}
	res, enableErr := restartWebForLAN()
	host, port := listenHostPort(st.Web.Listen)
	msg := formatWebLAN(on, host, port, listLANIPs(), ufwHint(port), res.Message)
	return msg, enableErr
}

// setWebLAN 把监听写成 0.0.0.0 或 127.0.0.1，端口保持不变。
func setWebLAN(on bool) (config.Settings, error) {
	st, err := config.LoadSettings()
	if err != nil {
		return config.Settings{}, err
	}
	_, port := listenHostPort(st.Web.Listen)
	if on {
		st.Web.Listen = net.JoinHostPort("0.0.0.0", port)
	} else {
		st.Web.Listen = net.JoinHostPort("127.0.0.1", port)
	}
	if err := config.SaveSettings(st); err != nil {
		return config.Settings{}, err
	}
	return st, nil
}

// listenHostPort 从 web.listen 取出地址和端口。缺省是 127.0.0.1:9800。
func listenHostPort(listen string) (string, string) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "127.0.0.1", "9800"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "127.0.0.1", "9800"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port
}

// WebLANOpen 判断当前监听是否对局域网开放。回环地址视为关闭。
func WebLANOpen(listen string) bool {
	host, _ := listenHostPort(listen)
	switch strings.Trim(host, "[]") {
	case "127.0.0.1", "::1", "localhost":
		return false
	default:
		return true
	}
}

// formatWebLAN 生成给用户看的说明。ufw 非空时附上放行命令，不会自己改防火墙。
func formatWebLAN(on bool, host, port string, ips []string, ufw, enableMsg string) string {
	var b strings.Builder
	if strings.TrimSpace(enableMsg) != "" {
		b.WriteString(strings.TrimRight(enableMsg, "\n"))
		b.WriteString("\n\n")
	}
	if on {
		fmt.Fprintf(&b, "已开启局域网访问，Web 监听 %s:%s\n", host, port)
		b.WriteString("只在可信网络上使用。\n\n")
		fmt.Fprintf(&b, "本机访问: http://127.0.0.1:%s\n", port)
		if len(ips) == 0 {
			b.WriteString("没有发现其他网卡地址。服务已在所有网卡上监听。\n")
		} else {
			b.WriteString("局域网访问:\n")
			for _, ip := range ips {
				fmt.Fprintf(&b, "  %s\n", lanURL(ip, port))
			}
		}
	} else {
		fmt.Fprintf(&b, "已关闭局域网访问，Web 只监听 %s:%s\n", host, port)
		fmt.Fprintf(&b, "地址: http://127.0.0.1:%s\n", port)
	}
	b.WriteString("登录令牌: partyctl web token\n")
	if on && strings.TrimSpace(ufw) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(ufw, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func lanURL(ip, port string) string {
	if strings.Contains(ip, ":") {
		return "http://[" + ip + "]:" + port
	}
	return "http://" + ip + ":" + port
}

// listLANIPs 返回已启用、非回环网卡上的单播地址，跳过链路本地地址。
func listLANIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

// ufwHint 在 ufw 可能拦截时给出放行命令。未安装或明确未启用时不提示。
func ufwHint(port string) string {
	installed, text := ufwStatus()
	return ufwHintFromStatus(installed, text, port)
}

func ufwStatus() (bool, string) {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false, ""
	}
	out, _ := exec.Command("ufw", "status").CombinedOutput()
	return true, string(out)
}

// ufwHintFromStatus 根据 ufw status 的输出决定要不要提示放行。
func ufwHintFromStatus(installed bool, text, port string) string {
	if !installed {
		return ""
	}
	low := strings.ToLower(text)
	if strings.Contains(low, "status: inactive") || strings.Contains(text, "不活动") || strings.Contains(text, "未激活") {
		return ""
	}
	line := fmt.Sprintf("  sudo ufw allow %s/tcp", port)
	if strings.Contains(low, "status: active") || strings.Contains(text, "激活") {
		return "本机 ufw 正在启用。如需放行 Web 端口，执行（不会自动修改防火墙）:\n" + line
	}
	return "如果 ufw 正在拦截局域网访问，执行（不会自动修改防火墙）:\n" + line
}
