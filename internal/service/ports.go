// 启动内核前检查 mixed-port 和外部控制器端口是否已被其他进程占用。
package service

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/procproxy"
)

// Listener 是一个正在监听的 TCP 端口及其进程。
type Listener struct {
	Addr string
	PID  int
	Cmd  string
}

// EnsurePortsFree 在 mihomo 绑定端口之前报告冲突。被占用时拒绝启动。
func EnsurePortsFree() error {
	st, err := config.LoadSettings()
	if err != nil {
		return err
	}
	var problems []string
	if msg := occupantMessage("外部控制器", st.ExternalController); msg != "" {
		problems = append(problems, msg)
	}
	mixedHost := "127.0.0.1"
	if st.AllowLan {
		mixedHost = "0.0.0.0"
	}
	mixed := net.JoinHostPort(mixedHost, strconv.Itoa(st.MixedPort))
	if msg := occupantMessage("mixed-port", mixed); msg != "" {
		problems = append(problems, msg)
	}
	if procproxy.Enabled() {
		if msg := occupantMessage("tproxy", net.JoinHostPort("127.0.0.1", strconv.Itoa(procproxy.TProxyPort))); msg != "" {
			problems = append(problems, msg)
		}
		dnsPort := procproxy.DNSPort
		if snap := procproxy.Snapshot(); snap.DNSPort > 0 {
			dnsPort = snap.DNSPort
		}
		if msg := occupantMessage("按进程 DNS", net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))); msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s\n可执行 ss -ltnp 查看监听。若要换端口: partyctl port set --mixed <端口> --controller <地址:端口>", strings.Join(problems, "\n"))
}

func occupantMessage(label, addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	ln, ok := findListener(host, port)
	if !ok {
		return ""
	}
	who := "未知进程"
	if ln.PID > 0 {
		who = fmt.Sprintf("pid %d", ln.PID)
		if ln.Cmd != "" {
			who += " " + ln.Cmd
		}
	}
	return fmt.Sprintf("%s %s 已被占用（%s）", label, addr, who)
}

// findListener 查找会和 bindHost:port 冲突的监听者。
func findListener(bindHost, port string) (Listener, bool) {
	portN, err := strconv.Atoi(port)
	if err != nil {
		return Listener{}, false
	}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, ln := range parseTCPListeners(string(data)) {
			if !conflicts(bindHost, portN, ln.ip, ln.port) {
				continue
			}
			pid, cmd := processForInode(ln.inode)
			return Listener{Addr: net.JoinHostPort(ln.ip.String(), strconv.Itoa(ln.port)), PID: pid, Cmd: cmd}, true
		}
	}
	return Listener{}, false
}

type tcpListener struct {
	ip    net.IP
	port  int
	inode string
}

func parseTCPListeners(data string) []tcpListener {
	var out []tcpListener
	for i, line := range strings.Split(data, "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[3] != "0A" { // LISTEN
			continue
		}
		ip, port, ok := parseProcAddr(fields[1])
		if !ok {
			continue
		}
		out = append(out, tcpListener{ip: ip, port: port, inode: fields[9]})
	}
	return out
}

func parseProcAddr(s string) (net.IP, int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return nil, 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return nil, 0, false
	}
	hexIP := parts[0]
	switch len(hexIP) {
	case 8:
		v, err := strconv.ParseUint(hexIP, 16, 32)
		if err != nil {
			return nil, 0, false
		}
		return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).To4(), int(port64), true
	case 32:
		b := make([]byte, 16)
		for i := 0; i < 4; i++ {
			word, err := strconv.ParseUint(hexIP[i*8:(i+1)*8], 16, 32)
			if err != nil {
				return nil, 0, false
			}
			b[i*4] = byte(word)
			b[i*4+1] = byte(word >> 8)
			b[i*4+2] = byte(word >> 16)
			b[i*4+3] = byte(word >> 24)
		}
		return net.IP(b), int(port64), true
	default:
		return nil, 0, false
	}
}

func conflicts(bindHost string, bindPort int, listenIP net.IP, listenPort int) bool {
	if bindPort != listenPort {
		return false
	}
	host := strings.Trim(bindHost, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	if listenIP.IsUnspecified() {
		return true
	}
	want := net.ParseIP(host)
	if want == nil {
		return false
	}
	return listenIP.Equal(want) || listenIP.Equal(want.To4()) || (want.To4() != nil && listenIP.Equal(want.To4()))
}

func processForInode(inode string) (int, string) {
	if inode == "" || inode == "0" {
		return 0, ""
	}
	target := "socket:[" + inode + "]"
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0, ""
	}
	for _, ent := range ents {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fdDir := filepath.Join("/proc", ent.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || link != target {
				continue
			}
			cmd, _ := os.ReadFile(filepath.Join("/proc", ent.Name(), "cmdline"))
			text := strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))
			return pid, text
		}
	}
	return 0, ""
}
