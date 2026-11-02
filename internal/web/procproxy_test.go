package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestProcProxyAPIAndPage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, err := New("127.0.0.1:0", "tok")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "按进程代理") {
		t.Fatalf("page %d missing card", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/proc-proxy", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"enabled"`) {
		t.Fatalf("list %d %s", rr.Code, rr.Body.String())
	}

	if os.Geteuid() == 0 {
		return
	}
	req = httptest.NewRequest(http.MethodPost, "/api/proc-proxy", bytes.NewReader([]byte(`{"pid":1}`)))
	req.Header.Set("Authorization", "Bearer tok")
	rr = httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "需要 root") {
		t.Fatalf("add without root: %d %s", rr.Code, rr.Body.String())
	}
}
