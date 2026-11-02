// nft 与策略路由的生成、安装和拆除。
// 本机内核不支持在 output 上做 TPROXY，所以先按 cgroup 打标，再经本地路由送到 prerouting。
package procproxy

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Render 生成只匹配接管 cgroup 的 nft 脚本。
func Render(routeMark, dnsPort int) string {
	if routeMark == 0 {
		routeMark = RouteMark
	}
	if dnsPort == 0 {
		dnsPort = DNSPort
	}
	return fmt.Sprintf(`table inet %s {
  chain prerouting {
    type filter hook prerouting priority mangle; policy accept;
    meta l4proto { tcp, udp } socket transparent 1 meta mark set 0x%x counter accept
    meta mark 0x%x meta l4proto tcp tproxy to :%d meta mark set 0x%x counter accept
    meta mark 0x%x meta l4proto udp tproxy to :%d meta mark set 0x%x counter accept
  }

  chain output {
    type route hook output priority mangle; policy accept;
    meta mark 0x%x counter return
    socket cgroupv2 level 1 "%s" th dport 53 counter return
    socket cgroupv2 level 1 "%s" ip daddr 127.0.0.0/8 counter return
    socket cgroupv2 level 1 "%s" ip6 daddr ::1 counter return
    socket cgroupv2 level 1 "%s" meta l4proto { tcp, udp } meta mark set 0x%x counter
  }

  chain dns {
    type nat hook output priority dstnat; policy accept;
    meta mark 0x%x counter return
    socket cgroupv2 level 1 "%s" meta l4proto { tcp, udp } th dport 53 counter redirect to :%d
  }
}
`, TableName,
		FwMark,
		FwMark, TProxyPort, FwMark,
		FwMark, TProxyPort, FwMark,
		routeMark,
		CgroupName,
		CgroupName,
		CgroupName,
		CgroupName, FwMark,
		routeMark,
		CgroupName, dnsPort)
}

// RulesPresent 查看 nft 表是否还在。没有 root 时当作不存在，避免普通用户的 status 报错。
func RulesPresent() bool {
	cmd := exec.Command("nft", "list", "table", "inet", TableName)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}
	msg := string(out)
	if strings.Contains(msg, "No such file") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "No such table") {
		return false
	}
	if os.Geteuid() != 0 {
		return false
	}
	return false
}

// Arm 在透明端口已监听时安装规则。重复调用是安全的。
func Arm() error {
	if err := RequireRoot(); err != nil {
		return err
	}
	if err := CheckSupport(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return armLocked()
}

// WaitAndArm 等待 mihomo 的 tproxy 与 DNS 端口，超时则保持直连。
func WaitAndArm() error {
	if !Enabled() {
		return nil
	}
	if !waitListeners(15 * time.Second) {
		_ = Disarm()
		return fmt.Errorf("透明代理端口 %d 或 DNS 端口未就绪，已不安装接管规则，进程保持直连", TProxyPort)
	}
	return Arm()
}

// Disarm 先删 nft 再删策略路由，让被接管进程回到直连，而不是黑洞。
func Disarm() error {
	if os.Geteuid() != 0 {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	return disarmLocked()
}

// Heal 在内核已停或端口消失时拆掉规则；内核在跑且仍有接管进程时把规则装回去。
func Heal(coreRunning bool) error {
	if os.Geteuid() != 0 {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	st := load()
	if !coreRunning || !st.Enabled || !listenersReady(st.DNSPort) {
		return disarmLocked()
	}
	if !hasLive(st) && cgroupEmpty() && len(st.Apps) == 0 {
		return nil
	}
	if RulesPresent() {
		return nil
	}
	return armLocked()
}

func armLocked() error {
	st := load()
	if err := ensureCgroup(); err != nil {
		return err
	}
	if err := installRoutes(); err != nil {
		_ = deleteRoutes()
		return err
	}
	if err := installNft(st); err != nil {
		_ = deleteNft()
		_ = deleteRoutes()
		return err
	}
	return nil
}

func disarmLocked() error {
	present := RulesPresent()
	nftErr := deleteNft()
	if present || routeInstalled() {
		_ = deleteRoutes()
	}
	return nftErr
}

func routeInstalled() bool {
	out, err := exec.Command("ip", "rule", "show", "pref", strconv.Itoa(RulePref)).CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	text := string(out)
	return strings.Contains(text, fmt.Sprintf("0x%x", FwMark)) || strings.Contains(text, strconv.Itoa(FwMark))
}

func installNft(st State) error {
	_ = deleteNft()
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(Render(st.RouteMark, st.DNSPort))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 nft 规则失败: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func deleteNft() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	cmd := exec.Command("nft", "delete", "table", "inet", TableName)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := string(out)
	if strings.Contains(msg, "No such file") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "No such table") {
		return nil
	}
	return fmt.Errorf("删除 nft 表 %s 失败: %v\n%s", TableName, err, strings.TrimSpace(string(out)))
}

func installRoutes() error {
	if err := deleteRoutes(); err != nil {
		return err
	}
	if err := runIP(false, "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", strconv.Itoa(RouteTable)); err != nil {
		return err
	}
	if err := runIP(false, "rule", "add", "fwmark", fmt.Sprintf("0x%x", FwMark), "lookup", strconv.Itoa(RouteTable), "pref", strconv.Itoa(RulePref)); err != nil {
		return err
	}
	if !ipv6On() {
		return nil
	}
	if err := runIP(true, "route", "replace", "local", "::/0", "dev", "lo", "table", strconv.Itoa(RouteTable)); err != nil {
		return err
	}
	return runIP(true, "rule", "add", "fwmark", fmt.Sprintf("0x%x", FwMark), "lookup", strconv.Itoa(RouteTable), "pref", strconv.Itoa(RulePref))
}

func deleteRoutes() error {
	_ = runIP(false, "rule", "del", "fwmark", fmt.Sprintf("0x%x", FwMark), "lookup", strconv.Itoa(RouteTable), "pref", strconv.Itoa(RulePref))
	_ = runIP(false, "route", "flush", "table", strconv.Itoa(RouteTable))
	if ipv6On() {
		_ = runIP(true, "rule", "del", "fwmark", fmt.Sprintf("0x%x", FwMark), "lookup", strconv.Itoa(RouteTable), "pref", strconv.Itoa(RulePref))
		_ = runIP(true, "route", "flush", "table", strconv.Itoa(RouteTable))
	}
	return nil
}

func runIP(v6 bool, args ...string) error {
	bin := "ip"
	full := args
	if v6 {
		full = append([]string{"-6"}, args...)
	}
	cmd := exec.Command(bin, full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s 失败: %v\n%s", strings.Join(full, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipv6On() bool {
	_, err := os.Stat("/proc/net/if_inet6")
	return err == nil
}

func waitListeners(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		st := func() State {
			mu.Lock()
			defer mu.Unlock()
			return load()
		}()
		port := st.DNSPort
		if port == 0 {
			port = DNSPort
		}
		if listenersReady(port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// PortsReady 确认 mihomo 已经在监听 tproxy 和本次使用的 DNS 端口。
func PortsReady() bool {
	mu.Lock()
	port := load().DNSPort
	mu.Unlock()
	return listenersReady(port)
}

// listenersReady 确认 mihomo 已经在监听 tproxy 和 DNS，避免规则指向一个没人接的端口。
func listenersReady(dnsPort int) bool {
	if dnsPort == 0 {
		dnsPort = DNSPort
	}
	return tcpListening(TProxyPort) && udpListening(dnsPort)
}

func tcpListening(port int) bool {
	return socketListening(port, true)
}

func udpListening(port int) bool {
	return socketListening(port, false)
}

func socketListening(port int, tcp bool) bool {
	paths := []string{"/proc/net/udp", "/proc/net/udp6"}
	if tcp {
		paths = []string{"/proc/net/tcp", "/proc/net/tcp6"}
	}
	want := fmt.Sprintf("%04X", port)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 || !strings.EqualFold(parts[1], want) {
				continue
			}
			if !tcp || fields[3] == "0A" {
				return true
			}
		}
	}
	return false
}
