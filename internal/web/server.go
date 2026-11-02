package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

//go:embed frontend/*
var frontendFS embed.FS

type Server struct {
	Listen string
	Token  string
	mux    *http.ServeMux
	http   *http.Server
}

func New(listen, token string) (*Server, error) {
	if listen == "" {
		listen = "127.0.0.1:9800"
	}
	if token == "" {
		return nil, fmt.Errorf("web token/secret is empty; refuse to start unauthenticated UI")
	}
	s := &Server{Listen: listen, Token: token, mux: http.NewServeMux()}
	s.routes()
	s.http = &http.Server{
		Addr:              listen,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

func NewFromSettings() (*Server, error) {
	st, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}
	return New(st.Web.Listen, config.WebAuthToken(st))
}

func (s *Server) routes() {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/status", s.handleStatus)
	api.HandleFunc("POST /api/core/{action}", s.handleCoreAction)
	api.HandleFunc("GET /api/settings", s.handleGetSettings)
	api.HandleFunc("PUT /api/settings", s.handlePutSettings)
	api.HandleFunc("GET /api/subs", s.handleListSubs)
	api.HandleFunc("POST /api/subs", s.handleAddSub)
	api.HandleFunc("POST /api/subs/{name}/update", s.handleUpdateSub)
	api.HandleFunc("POST /api/subs/{name}/use", s.handleUseSub)
	api.HandleFunc("DELETE /api/subs/{name}", s.handleDeleteSub)
	api.HandleFunc("GET /api/overrides", s.handleListOverrides)
	api.HandleFunc("GET /api/overrides/{name}", s.handleGetOverride)
	api.HandleFunc("PUT /api/overrides/{name}", s.handlePutOverride)
	api.HandleFunc("DELETE /api/overrides/{name}", s.handleDeleteOverride)
	api.HandleFunc("GET /api/proxies", s.handleProxies)
	api.HandleFunc("PUT /api/proxies/{group}", s.handleSelectProxy)
	api.HandleFunc("DELETE /api/proxies/{group}", s.handleUnfixProxy)
	api.HandleFunc("POST /api/diag", s.handleDiag)
	api.HandleFunc("GET /api/delay/{name}", s.handleDelay)
	api.HandleFunc("GET /api/traffic", s.handleTrafficSSE)
	api.HandleFunc("GET /api/logs", s.handleLogsSSE)
	api.HandleFunc("POST /api/generate", s.handleGenerate)
	api.HandleFunc("GET /api/proc-proxy", s.handleProcProxy)
	api.HandleFunc("POST /api/proc-proxy", s.handleProcProxyAdd)
	api.HandleFunc("DELETE /api/proc-proxy", s.handleProcProxyRemove)
	api.HandleFunc("POST /api/proc-proxy/clear", s.handleProcProxyClear)
	api.HandleFunc("POST /api/proc-proxy/app", s.handleProcProxyAppAdd)
	api.HandleFunc("DELETE /api/proc-proxy/app", s.handleProcProxyAppRemove)
	api.HandleFunc("POST /api/login", s.handleLogin)

	s.mux.Handle("/api/", s.authMiddleware(api))

	sub, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	s.mux.Handle("/", spaHandler(fileServer, sub))
}

func spaHandler(fileServer http.Handler, fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" && r.Method == http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		token := bearerToken(r)
		if token == "" {
			if c, err := r.Cookie("partyctl_token"); err == nil {
				token = c.Value
			}
		}
		if token == "" || token != s.Token {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const p = "Bearer "
	if strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

func (s *Server) ListenAndServe() error {
	host, _, err := net.SplitHostPort(s.Listen)
	if err == nil && host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "" {
		fmt.Fprintf(os.Stderr, "WARNING: web UI bound to non-loopback address %s — ensure network is trusted\n", s.Listen)
	}
	fmt.Fprintf(os.Stderr, "partyctl web listening on http://%s\n", s.Listen)
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	return s.http.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
