// Web 上的按进程代理入口。真正改规则仍需要 root。
package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/partyctl/partyctl/internal/procproxy"
	"github.com/partyctl/partyctl/internal/service"
)

func (s *Server) handleProcProxy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, procproxy.Snapshot())
}

func (s *Server) handleProcProxyAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PID  int   `json:"pid"`
		PIDs []int `json:"pids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	pids := body.PIDs
	if body.PID > 0 {
		pids = append(pids, body.PID)
	}
	msg, err := service.ProxyAdd(pids)
	if err != nil {
		writeProxyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "state": procproxy.Snapshot()})
}

func (s *Server) handleProcProxyRemove(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("pid")
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		writeErr(w, http.StatusBadRequest, "缺少 pid")
		return
	}
	msg, err := service.ProxyRemove(pid)
	if err != nil {
		writeProxyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "state": procproxy.Snapshot()})
}

func (s *Server) handleProcProxyClear(w http.ResponseWriter, r *http.Request) {
	msg, err := service.ProxyClear()
	if err != nil {
		writeProxyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "state": procproxy.Snapshot()})
}

func (s *Server) handleProcProxyAppAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Match string `json:"match"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Match) == "" {
		writeErr(w, http.StatusBadRequest, "请填写程序名或路径")
		return
	}
	msg, err := service.ProxyAppAdd(body.Match)
	if err != nil {
		writeProxyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "state": procproxy.Snapshot()})
}

func (s *Server) handleProcProxyAppRemove(w http.ResponseWriter, r *http.Request) {
	match := r.URL.Query().Get("match")
	msg, err := service.ProxyAppRemove(match)
	if err != nil {
		writeProxyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "state": procproxy.Snapshot()})
}

func writeProxyErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	code := http.StatusInternalServerError
	if strings.Contains(msg, "需要 root") {
		code = http.StatusForbidden
	}
	writeErr(w, code, msg)
}
