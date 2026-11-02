package service

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/procproxy"
	"gopkg.in/yaml.v3"
)

// TestProcProxyE2E 在 root 下确认：被接管进程走代理，其他进程直连，撤销和停止内核后恢复直连。
func TestProcProxyE2E(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("需要 root")
	}
	srcBin := "/home/ubuntu/.local/share/partyctl/mihomo"
	if _, err := os.Stat(srcBin); err != nil {
		t.Skip("本机没有 mihomo，跳过端到端")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("没有 nft")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PARTYCTL_FORCE_DETACH", "1")
	t.Setenv("PARTYCTL_MIRROR", "off")

	fakeAddr, fakeHits, closeFake := startFakeProxy(t)
	defer closeFake()

	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(srcBin, config.CoreBinaryPath()); err != nil {
		t.Fatal(err)
	}
	st := config.DefaultSettings()
	st.Secret = "e2esecret"
	st.CurrentProfile = "local"
	st.MixedPort = 17990
	st.ExternalController = "127.0.0.1:19190"
	st.Mode = "rule"
	st.TUNEnabled = false
	st.LogLevel = "info"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(fakeAddr)
	profile := map[string]any{
		"proxies":      []any{map[string]any{"name": "fake", "type": "http", "server": host, "port": atoiPort(port)}},
		"proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"fake"}}},
		"rules":        []any{"MATCH,PROXY"},
	}
	raw, _ := yaml.Marshal(profile)
	if err := os.MkdirAll(config.ProfilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProfilePath("local"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	py := filepath.Join(home, "agent.py")
	if err := os.WriteFile(py, []byte(agentPy), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "-u", py)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_, _ = procproxy.Clear()
		_ = Stop()
		_ = procproxy.Disarm()
	})

	ready := readLine(t, reader, 3*time.Second)
	t.Log(ready)
	var pid int
	if _, err := fmt.Sscanf(ready, "READY %d", &pid); err != nil || pid <= 0 {
		t.Fatalf("ready line %q: %v", ready, err)
	}

	before := ask(t, stdin, reader, "dns")
	if strings.Contains(before, "198.18.") {
		t.Fatalf("接管前就不该是 fake-ip: %s", before)
	}
	t.Log("before", before)

	msg, err := ProxyAdd([]int{pid})
	t.Log(msg)
	if err != nil {
		t.Fatal(err)
	}

	work, err := os.ReadFile(config.WorkConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(work), "tproxy-port: 7893") {
		t.Fatalf("运行配置没有 tproxy-port:\n%s", work)
	}
	var generated map[string]any
	if err := yaml.Unmarshal(work, &generated); err != nil {
		t.Fatal(err)
	}
	tun, _ := generated["tun"].(map[string]any)
	if tun["enable"] != false {
		t.Fatalf("tun 被改开了: %#v", tun)
	}
	for _, core := range ListOurCores() {
		cg, _ := procproxy.ReadCgroup(core.PID)
		if cg == "/partyctl" {
			t.Fatalf("mihomo %d 被放进了接管组", core.PID)
		}
	}

	var got string
	for i := 0; i < 8; i++ {
		got = ask(t, stdin, reader, "dns")
		t.Log("proxied dns", got)
		if strings.Contains(got, "198.18.") {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !strings.Contains(got, "198.18.") {
		t.Fatalf("被接管进程的 DNS 没有进 mihomo: %s", got)
	}

	direct := directDNS(t)
	t.Log("direct dns", direct)
	if strings.Contains(direct, "198.18.") {
		t.Fatalf("未接管进程也被劫持了 DNS: %s", direct)
	}

	hitsBefore := fakeHits()
	tcpLine := ask(t, stdin, reader, "tcp")
	t.Log("proxied tcp", tcpLine)
	if !strings.Contains(tcpLine, "TCP") || strings.Contains(tcpLine, "TCP_ERR") {
		t.Fatalf("被接管进程 TCP 失败: %s", tcpLine)
	}
	if !waitHits(fakeHits, hitsBefore, 3*time.Second) {
		out, _ := exec.Command("nft", "list", "table", "inet", "partyctl").CombinedOutput()
		t.Logf("nft:\n%s", out)
		t.Logf("connections:\n%s", truncate(controllerBody(t, st, "/connections"), 1500))
		t.Fatalf("代理上游没有看到 CONNECT，hits %d -> %d", hitsBefore, fakeHits())
	}
	time.Sleep(200 * time.Millisecond)
	directHits := fakeHits()
	directTCPLine := directTCP(t)
	t.Log("direct tcp", directTCPLine)
	if fakeHits() != directHits {
		t.Fatalf("未接管进程也进了代理 hits %d -> %d", directHits, fakeHits())
	}

	udpBefore := nftPackets(t, "udp tproxy")
	_ = ask(t, stdin, reader, "udp")
	time.Sleep(400 * time.Millisecond)
	udpAfter := nftPackets(t, "udp tproxy")
	t.Logf("udp tproxy packets %d -> %d", udpBefore, udpAfter)
	if udpAfter <= udpBefore {
		t.Fatal("UDP 没有被 tproxy 接管")
	}
	body := controllerBody(t, st, "/connections")
	if !strings.Contains(body, "1.1.1.1") {
		t.Logf("connections 里暂时没有 1.1.1.1:\n%s", truncate(body, 800))
	}

	childHits := fakeHits()
	child := ask(t, stdin, reader, "child")
	t.Log("child", child)
	if !strings.Contains(child, "CHILD_TCP") {
		t.Fatalf("子进程连接失败: %s", child)
	}
	if !waitHits(fakeHits, childHits, 3*time.Second) {
		t.Fatal("子进程没有走代理")
	}

	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if procproxy.RulesPresent() {
		t.Fatal("内核停止后规则还在，进程可能断网")
	}
	stoppedHits := fakeHits()
	openLine := ask(t, stdin, reader, "tcp")
	t.Log("after stop", openLine)
	if strings.Contains(openLine, "TCP_ERR") {
		t.Fatalf("停止内核后被接管进程没能恢复直连: %s", openLine)
	}
	if fakeHits() != stoppedHits {
		t.Fatalf("停止后流量仍进代理 hits %d -> %d", stoppedHits, fakeHits())
	}

	if err := StartDetached(); err != nil {
		t.Fatal(err)
	}
	var restored string
	for i := 0; i < 10; i++ {
		restored = ask(t, stdin, reader, "dns")
		if strings.Contains(restored, "198.18.") && procproxy.RulesPresent() {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Log("restored", restored, "rules", procproxy.RulesPresent())
	if !strings.Contains(restored, "198.18.") || !procproxy.RulesPresent() {
		t.Fatalf("内核重新启动后没有恢复接管: %s rules=%v", restored, procproxy.RulesPresent())
	}

	rmMsg, err := ProxyRemove(pid)
	t.Log(rmMsg)
	if err != nil {
		t.Fatal(err)
	}
	after := ask(t, stdin, reader, "dns")
	t.Log("after rm", after)
	if strings.Contains(after, "198.18.") {
		t.Fatalf("撤销后仍走 fake-ip: %s", after)
	}
	rmHits := fakeHits()
	_ = ask(t, stdin, reader, "tcp")
	if fakeHits() != rmHits {
		t.Fatal("撤销后 TCP 仍走代理")
	}

	clearMsg, err := ProxyClear()
	t.Log(clearMsg)
	if err != nil {
		t.Fatal(err)
	}
	if procproxy.RulesPresent() {
		t.Fatal("clear 后规则还在")
	}
	cleared, err := os.ReadFile(config.WorkConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cleared), "tproxy-port:") {
		t.Fatalf("clear 后运行配置仍有 tproxy-port:\n%s", cleared)
	}
}

func startFakeProxy(t *testing.T) (addr string, hits func() int, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var n int
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnect(t, c, func() {
				mu.Lock()
				n++
				mu.Unlock()
			})
		}
	}()
	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}, func() { _ = ln.Close() }
}

func handleConnect(t *testing.T, c net.Conn, hit func()) {
	defer c.Close()
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	if strings.HasPrefix(line, "CONNECT ") {
		hit()
		t.Logf("fake CONNECT %s", strings.TrimSpace(line))
	}
	for {
		h, err := br.ReadString('\n')
		if err != nil || h == "\r\n" || h == "\n" {
			break
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(line, "CONNECT ") {
		return
	}
	up, err := net.DialTimeout("tcp", fields[1], 4*time.Second)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer up.Close()
	_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, br); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
	<-done
}

func atoiPort(port string) int {
	n := 0
	for _, c := range port {
		n = n*10 + int(c-'0')
	}
	return n
}

func readLine(t *testing.T, r *bufio.Reader, d time.Duration) string {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := r.ReadString('\n')
		ch <- result{strings.TrimSpace(s), err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatal(res.err)
		}
		return res.s
	case <-time.After(d):
		t.Fatal("读取超时")
		return ""
	}
}

func ask(t *testing.T, stdin io.Writer, r *bufio.Reader, cmd string) string {
	t.Helper()
	if _, err := fmt.Fprintln(stdin, cmd); err != nil {
		t.Fatal(err)
	}
	return readLine(t, r, 6*time.Second)
}

func directDNS(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("python3", "-c", `import socket; print(",".join(sorted({i[4][0] for i in socket.getaddrinfo("example.com", 80)})))`).CombinedOutput()
	if err != nil {
		t.Fatalf("direct dns: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func directTCP(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("python3", "-c", `import socket
s=socket.create_connection(("1.1.1.1",80),3)
print("DIRECT", s.getpeername())
s.close()`).CombinedOutput()
	if err != nil {
		t.Fatalf("direct tcp: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func nftPackets(t *testing.T, kind string) int {
	t.Helper()
	out, err := exec.Command("nft", "list", "table", "inet", "partyctl").CombinedOutput()
	if err != nil {
		t.Fatalf("nft list: %v %s", err, out)
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, kind) && strings.Contains(line, "packets ") {
			fmt.Sscanf(line[strings.Index(line, "packets "):], "packets %d", &n)
		}
	}
	return n
}

func controllerBody(t *testing.T, st config.Settings, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+st.ExternalController+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		t.Log("controller", err)
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b)
}

func waitHits(hits func() int, before int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if hits() > before {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return hits() > before
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

const agentPy = `
import os, socket, sys
print("READY", os.getpid(), flush=True)
for line in sys.stdin:
    line = line.strip()
    if line == "dns":
        try:
            ips = sorted({i[4][0] for i in socket.getaddrinfo("example.com", 80)})
            print("DNS", ",".join(ips), flush=True)
        except Exception as e:
            print("DNS_ERR", e, flush=True)
    elif line == "tcp":
        try:
            s = socket.create_connection(("1.1.1.1", 80), 4)
            print("TCP", s.getpeername(), flush=True)
            s.close()
        except Exception as e:
            print("TCP_ERR", type(e).__name__, e, flush=True)
    elif line == "udp":
        try:
            u = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            u.sendto(b"partyctl-udp", ("1.1.1.1", 443))
            print("UDP_SENT", flush=True)
        except Exception as e:
            print("UDP_ERR", e, flush=True)
    elif line == "child":
        pid = os.fork()
        if pid == 0:
            try:
                s = socket.create_connection(("1.1.1.1", 80), 4)
                print("CHILD_TCP", os.getpid(), flush=True)
                s.close()
            except Exception as e:
                print("CHILD_ERR", e, flush=True)
            os._exit(0)
        os.waitpid(pid, 0)
    else:
        print("UNKNOWN", line, flush=True)
`
