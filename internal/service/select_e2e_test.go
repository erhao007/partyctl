package service

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/config"
	"gopkg.in/yaml.v3"
)

func TestURLTestFixedSurvivesUntilUnfix(t *testing.T) {
	src := os.Getenv("MIHOMO_BIN")
	if src == "" {
		src = "/home/ubuntu/.local/share/partyctl/mihomo"
	}
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		t.Skip("no mihomo")
	}
	t.Setenv("PARTYCTL_FORCE_DETACH", "1")
	t.Setenv("PARTYCTL_MIRROR", "off")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, config.CoreBinaryPath()); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctrlPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mixed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	st := config.DefaultSettings()
	st.Secret = "fixedsecret"
	st.CurrentProfile = "local"
	st.MixedPort = mixed
	st.ExternalController = "127.0.0.1:" + itoa(ctrlPort)
	st.Mode = "rule"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{
		"proxies": []any{
			map[string]any{"name": "node-a", "type": "direct"},
			map[string]any{"name": "node-b", "type": "direct"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "自动选择", "type": "url-test", "proxies": []any{"node-a", "node-b"}, "url": "http://127.0.0.1:1", "interval": 3600},
		},
		"rules": []any{"MATCH,自动选择"},
	}
	raw, _ := yaml.Marshal(profile)
	if err := os.MkdirAll(filepath.Dir(config.ProfilePath("local")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProfilePath("local"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StartDetached(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })

	c, err := client.New()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	var info map[string]any
	for time.Now().Before(deadline) {
		proxies, err := c.Proxies()
		if err == nil {
			pm, _ := proxies["proxies"].(map[string]any)
			info, _ = pm["自动选择"].(map[string]any)
			manual, _ := pm["手动选择"].(map[string]any)
			if info != nil && manual != nil {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
		info = nil
	}
	if info == nil {
		t.Fatal("url-test group not ready")
	}
	if err := c.SelectProxy("自动选择", "node-b"); err != nil {
		t.Fatal(err)
	}
	proxies, err := c.Proxies()
	if err != nil {
		t.Fatal(err)
	}
	pm := proxies["proxies"].(map[string]any)
	info = pm["自动选择"].(map[string]any)
	if client.FixedProxy(info) != "node-b" {
		t.Fatalf("fixed = %#v now=%v", info["fixed"], info["now"])
	}
	if err := Restart(false); err != nil {
		t.Fatal(err)
	}
	c, err = client.New()
	if err != nil {
		t.Fatal(err)
	}
	info = nil
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		proxies, err = c.Proxies()
		if err == nil {
			pm, _ = proxies["proxies"].(map[string]any)
			info, _ = pm["自动选择"].(map[string]any)
			if client.FixedProxy(info) == "node-b" {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if client.FixedProxy(info) != "node-b" {
		t.Fatalf("fixed lost after restart: %#v", info)
	}
	if err := c.UnfixProxy("自动选择"); err != nil {
		t.Fatal(err)
	}
	proxies, err = c.Proxies()
	if err != nil {
		t.Fatal(err)
	}
	info = proxies["proxies"].(map[string]any)["自动选择"].(map[string]any)
	if client.FixedProxy(info) != "" {
		t.Fatalf("still fixed: %#v", info["fixed"])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
