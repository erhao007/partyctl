package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProxiesRejectsHTMLAndIgnoresProxy(t *testing.T) {
	var proxyHits int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>via proxy</html>"))
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>not json</body></html>"))
	}))
	t.Cleanup(controller.Close)

	c := &Client{Base: controller.URL, Secret: "s", HTTP: directClient(5 * time.Second)}
	_, err := c.Proxies()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 200") || !strings.Contains(msg, "不是 JSON") || !strings.Contains(msg, controller.URL+"/proxies") {
		t.Fatalf("error: %s", msg)
	}
	if strings.Contains(msg, "invalid character") {
		t.Fatalf("still a raw json error: %s", msg)
	}
	if proxyHits != 0 {
		t.Fatalf("request went through HTTP_PROXY (%d hits)", proxyHits)
	}
}

func TestProxiesRejectsForeignJSON(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing secret: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(controller.Close)
	c := &Client{Base: controller.URL, Secret: "secret", HTTP: directClient(5 * time.Second)}
	_, err := c.Proxies()
	if err == nil || !strings.Contains(err.Error(), "不是 mihomo") {
		t.Fatalf("error: %v", err)
	}
}

func TestProxiesControllerDown(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:1", Secret: "s", HTTP: directClient(2 * time.Second)}
	_, err := c.Proxies()
	if err == nil || !strings.Contains(err.Error(), "内核未运行，请先启动") {
		t.Fatalf("error: %v", err)
	}
}
