// 诊断接口把内核、端口和网站连通性汇总给 Web。
package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/diag"
)

func (s *Server) handleDiag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URLs    []string `json:"urls"`
		Timeout int      `json:"timeout"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	timeout := 8 * time.Second
	if body.Timeout > 0 && body.Timeout <= 30 {
		timeout = time.Duration(body.Timeout) * time.Second
	}
	rep, err := diag.Run(body.URLs, timeout)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleUnfixProxy(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	c, err := client.New()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := c.UnfixProxy(group); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "group": group})
}
