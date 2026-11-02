package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
	"gopkg.in/yaml.v3"
)

func TestAuthAndStatus(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st := config.DefaultSettings()
	st.Secret = "test-secret-token"
	st.CurrentProfile = "local"
	st.Web.Listen = "127.0.0.1:9800"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{
		"proxies": []any{map[string]any{"name": "DIRECT-NODE", "type": "direct"}},
		"proxy-groups": []any{map[string]any{
			"name": "PROXY", "type": "select", "proxies": []any{"DIRECT", "DIRECT-NODE"},
		}},
		"rules": []any{"MATCH,PROXY"},
	}
	raw, _ := yaml.Marshal(profile)
	if err := os.WriteFile(config.ProfilePath("local"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	meta := config.ProfilesFile{Profiles: []config.ProfileMeta{{
		Name: "local",
		SubscriptionUserinfo: "upload=100; download=200; total=1000; expire=1893456000",
	}}}
	if err := config.SaveProfilesMeta(meta); err != nil {
		t.Fatal(err)
	}

	srv, err := New("127.0.0.1:0", st.Secret)
	if err != nil {
		t.Fatal(err)
	}

	// unauthorized
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	// login
	body, _ := json.Marshal(map[string]string{"token": st.Secret})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("login: %d %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	var status map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &status)
	if status["current_profile"] != "local" {
		t.Fatalf("profile: %#v", status)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/subs", nil)
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("subs: %d %s", rr.Code, rr.Body.String())
	}
	var subs map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &subs)
	profiles, _ := subs["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("profiles: %#v", subs)
	}
	p0 := profiles[0].(map[string]any)
	ui := p0["userinfo"].(map[string]any)
	if ui["used"] != float64(300) && ui["used"] != int64(300) {
		// json numbers are float64
		if used, ok := ui["used"].(float64); !ok || used != 300 {
			t.Fatalf("userinfo used: %#v", ui)
		}
	}

	// settings put
	put, _ := json.Marshal(map[string]any{"mode": "global", "allow_lan": true})
	req = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(put))
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("settings put: %d %s", rr.Code, rr.Body.String())
	}
	st2, _ := config.LoadSettings()
	if st2.Mode != "global" || !st2.AllowLan {
		t.Fatalf("settings not saved: %#v", st2)
	}

	// override put/get
	ov, _ := json.Marshal(map[string]string{"content": "+rules:\n  - DOMAIN,test.local,DIRECT\n"})
	req = httptest.NewRequest(http.MethodPut, "/api/overrides/webtest.yaml", bytes.NewReader(ov))
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("override put: %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/overrides/webtest.yaml", nil)
	req.Header.Set("Authorization", "Bearer "+st.Secret)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("override get: %d", rr.Code)
	}

	// static index
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte("partyctl")) {
		t.Fatalf("index: %d", rr.Code)
	}
}

func TestParseUserinfo(t *testing.T) {
	ui := parseUserinfo("upload=1; download=2; total=10; expire=1893456000")
	if ui["used"] != int64(3) {
		t.Fatalf("%#v", ui)
	}
	if ui["expire_at"] == nil || ui["expire_at"] == "" {
		t.Fatalf("expire_at missing: %#v", ui)
	}
}

func TestSanitizeName(t *testing.T) {
	got := sanitizeName("../x")
	if got != "x.yaml" {
		t.Fatalf("got %q", got)
	}
	if sanitizeName("a.yml") != "a.yml" {
		t.Fatalf("yml suffix: %q", sanitizeName("a.yml"))
	}
}
