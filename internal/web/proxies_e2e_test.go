package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/service"
)

// TestNodesWithRealMihomo 用一份常见订阅结构启动真实 mihomo，确认策略组和节点能从控制器读出来。
func TestNodesWithRealMihomo(t *testing.T) {
	src := os.Getenv("MIHOMO_BIN")
	if src == "" {
		src = filepath.Join(os.Getenv("HOME"), ".local", "share", "partyctl", "mihomo")
	}
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		t.Skip("real mihomo binary not available")
	}
	t.Setenv("PARTYCTL_FORCE_DETACH", "1")
	t.Setenv("PARTYCTL_MIRROR", "off")
	home := t.TempDir()
	t.Setenv("HOME", home)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if err := os.MkdirAll(filepath.Dir(config.CoreBinaryPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, config.CoreBinaryPath()); err != nil {
		t.Fatal(err)
	}
	st := config.DefaultSettings()
	st.Secret = "e2e-secret"
	st.MixedPort = port + 1
	if st.MixedPort > 65535 {
		st.MixedPort = port - 1
	}
	st.ExternalController = fmt.Sprintf("127.0.0.1:%d", port)
	st.Web.Listen = "127.0.0.1:9800"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}

	sub := []byte(`proxies:
  - name: node-a
    type: http
    server: 127.0.0.1
    port: 9
  - name: node-b
    type: socks5
    server: 127.0.0.1
    port: 9
proxy-groups:
  - name: PROXY
    type: select
    proxies: [node-a, node-b, DIRECT]
  - name: Auto
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies: [node-a, node-b]
rules:
  - MATCH,PROXY
`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sub)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() { _ = service.Stop() })

	srv, err := New("127.0.0.1:0", st.Secret)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+st.Secret)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)
		return rr
	}

	body, _ := json.Marshal(map[string]string{"name": "airport", "url": upstream.URL + "/clash.yaml"})
	rr := call(http.MethodPost, "/api/subs", body)
	if rr.Code != 200 {
		t.Fatalf("add: %d %s", rr.Code, rr.Body.String())
	}
	var added map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &added)
	if added["started"] != true {
		t.Fatalf("core did not start: %#v", added)
	}

	var proxies map[string]any
	deadline := time.Now().Add(8 * time.Second)
	for {
		rr = call(http.MethodGet, "/api/proxies", nil)
		if rr.Code == 200 {
			_ = json.Unmarshal(rr.Body.Bytes(), &proxies)
			if pm, _ := proxies["proxies"].(map[string]any); pm != nil {
				if _, ok := pm["PROXY"]; ok {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxies not ready: %d %s", rr.Code, rr.Body.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	pm := proxies["proxies"].(map[string]any)
	proxy := pm["PROXY"].(map[string]any)
	all, _ := proxy["all"].([]any)
	names := map[string]bool{}
	for _, n := range all {
		names[fmt.Sprint(n)] = true
	}
	if !names["node-a"] || !names["node-b"] || !names["DIRECT"] {
		t.Fatalf("PROXY nodes: %#v", proxy["all"])
	}
	auto := pm["Auto"].(map[string]any)
	if fmt.Sprint(auto["type"]) == "" {
		t.Fatalf("Auto group: %#v", auto)
	}
	if _, ok := auto["all"]; !ok {
		t.Fatalf("Auto has no nodes: %#v", auto)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	return exec.Command("chmod", "+x", dst).Run()
}
