package procproxy

import (
	"os"
	"strings"
	"testing"
)

func TestRenderCoversTCPUDPAndDNS(t *testing.T) {
	script := Render(0x2518, 1053)
	for _, want := range []string{
		`socket cgroupv2 level 1 "partyctl"`,
		"tproxy to :7893",
		"redirect to :1053",
		"meta mark 0x2518 counter return",
		"th dport 53 counter return",
		"127.0.0.0/8",
		"ip6 daddr ::1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n%s", want, script)
		}
	}
	if strings.Contains(script, "tun") {
		t.Fatalf("nft script must not touch tun\n%s", script)
	}
}

func TestRequireRootMessage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("当前是 root")
	}
	err := RequireRoot()
	if err == nil || !strings.Contains(err.Error(), "需要 root") || !strings.Contains(err.Error(), "sudo partyctl") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyRuntimeDisabledDoesNotInject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	profile := map[string]any{"tun": map[string]any{"enable": true}}
	ApplyRuntime(profile)
	if _, ok := profile["tproxy-port"]; ok {
		t.Fatalf("disabled run injected tproxy: %#v", profile)
	}
	tun := profile["tun"].(map[string]any)
	if tun["enable"] != true {
		t.Fatalf("tun changed: %#v", tun)
	}
}

func TestApplyRuntimeKeepsExistingDNSAndTun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{
		"tun": map[string]any{"enable": true, "stack": "system"},
		"dns": map[string]any{
			"enable":     false,
			"nameserver": []any{"https://example.invalid/dns-query"},
			"listen":     "127.0.0.1:5353",
		},
		"routing-mark": 4321,
	}
	ApplyRuntime(profile)
	tun := profile["tun"].(map[string]any)
	if tun["enable"] != true || tun["stack"] != "system" {
		t.Fatalf("tun changed: %#v", tun)
	}
	if profile["tproxy-port"] != TProxyPort {
		t.Fatalf("tproxy-port %#v", profile["tproxy-port"])
	}
	if profile["routing-mark"] != 4321 {
		t.Fatalf("routing-mark overwritten: %#v", profile["routing-mark"])
	}
	dns := profile["dns"].(map[string]any)
	if dns["listen"] != "127.0.0.1:5353" {
		t.Fatalf("listen changed: %#v", dns["listen"])
	}
	if dns["enable"] != true {
		t.Fatalf("dns not enabled: %#v", dns["enable"])
	}
	ns := dns["nameserver"].([]any)
	if len(ns) != 1 || ns[0] != "https://example.invalid/dns-query" {
		t.Fatalf("nameserver changed: %#v", ns)
	}
	if _, ok := profile["redir-port"]; ok {
		t.Fatal("unexpected redir-port")
	}
}

func TestApplyRuntimeInjectsDNSWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{"tun": map[string]any{"enable": false}}
	ApplyRuntime(profile)
	dns := profile["dns"].(map[string]any)
	if dns["enhanced-mode"] != "fake-ip" {
		t.Fatalf("dns %#v", dns)
	}
	if dns["listen"] != "127.0.0.1:1053" {
		t.Fatalf("listen %#v", dns["listen"])
	}
	if profile["routing-mark"] != RouteMark {
		t.Fatalf("mark %#v", profile["routing-mark"])
	}
}

func TestStateRoundTripAndDescribe(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if Enabled() {
		t.Fatal("empty home should be disabled")
	}
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if got := Describe(); !strings.Contains(got, "开") {
		t.Fatalf("describe %s", got)
	}
	text := Snapshot().Text()
	if !strings.Contains(text, "当前没有接管任何进程") {
		t.Fatalf("text %s", text)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if Describe() != "关" {
		t.Fatalf("describe after disable %s", Describe())
	}
}

func TestReadCgroupSelf(t *testing.T) {
	cg, err := ReadCgroup(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if cg == "" {
		t.Fatal("empty cgroup")
	}
}
