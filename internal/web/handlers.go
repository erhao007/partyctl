package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/core"
	"github.com/partyctl/partyctl/internal/service"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Token != s.Token {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "partyctl_token",
		Value:    body.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	running := service.IsRunning()
	coreInstalled := false
	coreVersion := ""
	if ver, err := core.Version(); err == nil {
		coreInstalled = strings.TrimSpace(ver) != ""
		if i := strings.IndexByte(ver, '\n'); i >= 0 {
			ver = ver[:i]
		}
		coreVersion = strings.TrimSpace(ver)
	}
	// best-effort version via controller when the core is up
	var mihomo map[string]any
	if c, err := client.New(); err == nil {
		if v, err := c.Version(); err == nil {
			mihomo = v
			running = true
		}
	}
	writeJSON(w, 200, map[string]any{
		"running":             running,
		"mode":                st.Mode,
		"current_profile":     st.CurrentProfile,
		"mixed_port":          st.MixedPort,
		"external_controller": st.ExternalController,
		"tun":                 st.TUNEnabled,
		"allow_lan":           st.AllowLan,
		"log_level":           st.LogLevel,
		"web_listen":          st.Web.Listen,
		"core_path":           config.CoreBinaryPath(),
		"core_installed":      coreInstalled,
		"core_version":        coreVersion,
		"mihomo":              mihomo,
	})
}

func (s *Server) handleCoreAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	var err error
	switch action {
	case "start":
		err = service.StartBackground()
	case "stop":
		err = service.Stop()
	case "restart":
		err = service.RestartBackground()
	default:
		writeErr(w, 400, "action must be start|stop|restart")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "action": action})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"mixed_port":          st.MixedPort,
		"external_controller": st.ExternalController,
		"mode":                st.Mode,
		"tun":                 st.TUNEnabled,
		"allow_lan":           st.AllowLan,
		"log_level":           st.LogLevel,
		"delay_test_url":      st.DelayTestURL,
		"delay_test_timeout":  st.DelayTestTimeout,
		"current_profile":     st.CurrentProfile,
		"overrides":           st.Overrides,
		"web": map[string]any{
			"enabled": st.Web.Enabled,
			"listen":  st.Web.Listen,
			// never return token/secret values in full — only whether set
			"token_set": st.Web.Token != "" || st.Secret != "",
		},
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if v, ok := asInt(body["mixed_port"]); ok {
		st.MixedPort = v
	}
	if v, ok := body["external_controller"].(string); ok && v != "" {
		st.ExternalController = v
	}
	if v, ok := body["mode"].(string); ok && v != "" {
		st.Mode = v
	}
	if v, ok := body["tun"].(bool); ok {
		st.TUNEnabled = v
	}
	if v, ok := body["allow_lan"].(bool); ok {
		st.AllowLan = v
	}
	if v, ok := body["log_level"].(string); ok && v != "" {
		st.LogLevel = v
	}
	if v, ok := body["delay_test_url"].(string); ok && v != "" {
		st.DelayTestURL = v
	}
	if v, ok := asInt(body["delay_test_timeout"]); ok {
		st.DelayTestTimeout = v
	}
	if web, ok := body["web"].(map[string]any); ok {
		if v, ok := web["enabled"].(bool); ok {
			st.Web.Enabled = v
		}
		if v, ok := web["listen"].(string); ok && v != "" {
			st.Web.Listen = v
		}
		if v, ok := web["token"].(string); ok {
			st.Web.Token = v
		}
	}
	if err := config.SaveSettings(st); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	path, err := config.Generate()
	if err != nil {
		writeErr(w, 500, "saved but generate failed: "+err.Error())
		return
	}
	// live patch mode/port if core up
	if c, err := client.New(); err == nil {
		patch := map[string]any{"mode": st.Mode, "allow-lan": st.AllowLan}
		_ = c.PatchConfigs(patch)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "config": path})
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseUserinfo(raw string) map[string]any {
	out := map[string]any{"raw": raw}
	if raw == "" {
		return out
	}
	parts := strings.Split(raw, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			out[k] = n
		} else {
			out[k] = v
		}
	}
	upload, _ := out["upload"].(int64)
	download, _ := out["download"].(int64)
	total, _ := out["total"].(int64)
	out["used"] = upload + download
	if total > 0 {
		out["used_ratio"] = float64(upload+download) / float64(total)
	}
	if exp, ok := out["expire"].(int64); ok && exp > 0 {
		out["expire_at"] = time.Unix(exp, 0).Format(time.RFC3339)
	}
	return out
}

func (s *Server) handleListSubs(w http.ResponseWriter, r *http.Request) {
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	meta, err := config.LoadProfilesMeta()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(meta.Profiles))
	for _, p := range meta.Profiles {
		items = append(items, map[string]any{
			"name":     p.Name,
			"url":      p.URL,
			"updated":  p.Updated,
			"current":  p.Name == st.CurrentProfile,
			"userinfo": parseUserinfo(p.SubscriptionUserinfo),
			"has_file": fileExists(config.ProfilePath(p.Name)),
		})
	}
	writeJSON(w, 200, map[string]any{"profiles": items, "current": st.CurrentProfile})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func downloadSub(name, rawURL string) (string, error) {
	clientHTTP := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "partyctl/clash.meta")
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download: %s: %s", resp.Status, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	if err := os.WriteFile(config.ProfilePath(name), body, 0o644); err != nil {
		return "", err
	}
	return resp.Header.Get("Subscription-Userinfo"), nil
}

func (s *Server) handleAddSub(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		writeErr(w, 400, "name and url required")
		return
	}
	meta, err := config.LoadProfilesMeta()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if _, _, ok := config.FindProfile(meta, body.Name); ok {
		writeErr(w, 409, "profile exists")
		return
	}
	userinfo, err := downloadSub(body.Name, body.URL)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	meta.Profiles = append(meta.Profiles, config.ProfileMeta{
		Name: body.Name, URL: body.URL,
		Updated:              time.Now().Format(time.RFC3339),
		SubscriptionUserinfo: userinfo,
	})
	if err := config.SaveProfilesMeta(meta); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	activated := false
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 第一份订阅直接启用，这样用户添加后就能启动内核。
	if st.CurrentProfile == "" {
		st.CurrentProfile = body.Name
		if err := config.SaveSettings(st); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if _, err := config.Generate(); err != nil {
			writeErr(w, 500, "订阅已保存，但生成配置失败: "+err.Error())
			return
		}
		activated = true
	}
	started := false
	warning := ""
	// 第一份订阅启用后把内核拉起来，节点页才有策略组可读。启动失败不影响订阅已保存。
	if activated {
		var startErr error
		if service.IsRunning() {
			startErr = service.RestartBackground()
		} else {
			startErr = service.StartBackground()
		}
		if startErr != nil {
			warning = "订阅已启用，但启动内核失败: " + startErr.Error()
		} else {
			started = true
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "name": body.Name, "activated": activated, "started": started, "warning": warning,
	})
}

func (s *Server) handleUpdateSub(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	meta, err := config.LoadProfilesMeta()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	p, idx, ok := config.FindProfile(meta, name)
	if !ok {
		writeErr(w, 404, "not found")
		return
	}
	if p.URL == "" {
		writeErr(w, 400, "local profile has no url")
		return
	}
	userinfo, err := downloadSub(name, p.URL)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	meta.Profiles[idx].Updated = time.Now().Format(time.RFC3339)
	meta.Profiles[idx].SubscriptionUserinfo = userinfo
	if err := config.SaveProfilesMeta(meta); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "userinfo": parseUserinfo(userinfo)})
}

func (s *Server) handleUseSub(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	meta, err := config.LoadProfilesMeta()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if _, _, ok := config.FindProfile(meta, name); !ok {
		if !fileExists(config.ProfilePath(name)) {
			writeErr(w, 404, "not found")
			return
		}
		meta.Profiles = append(meta.Profiles, config.ProfileMeta{Name: name})
		_ = config.SaveProfilesMeta(meta)
	}
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st.CurrentProfile = name
	if err := config.SaveSettings(st); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	path, err := config.Generate()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	restarted := false
	if service.IsRunning() {
		if err := service.RestartBackground(); err != nil {
			writeErr(w, 500, "已切换到 "+name+"，但重启内核失败: "+err.Error())
			return
		}
		restarted = true
	}
	writeJSON(w, 200, map[string]any{"ok": true, "current": name, "config": path, "restarted": restarted})
}

func (s *Server) handleDeleteSub(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	meta, err := config.LoadProfilesMeta()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, idx, ok := config.FindProfile(meta, name)
	if !ok {
		writeErr(w, 404, "not found")
		return
	}
	meta.Profiles = append(meta.Profiles[:idx], meta.Profiles[idx+1:]...)
	_ = config.SaveProfilesMeta(meta)
	_ = os.Remove(config.ProfilePath(name))
	st, _ := config.LoadSettings()
	if st.CurrentProfile == name {
		st.CurrentProfile = ""
		_ = config.SaveSettings(st)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListOverrides(w http.ResponseWriter, r *http.Request) {
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	items := []map[string]any{}
	for _, name := range st.Overrides {
		items = append(items, map[string]any{"name": name})
	}
	writeJSON(w, 200, map[string]any{"overrides": items})
}

func (s *Server) handleGetOverride(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	data, err := os.ReadFile(filepath.Join(config.OverrideDir(), name))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "content": string(data)})
}

func (s *Server) handlePutOverride(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if err := config.EnsureDirs(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	path := filepath.Join(config.OverrideDir(), name)
	if err := os.WriteFile(path, []byte(body.Content), 0o644); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	found := false
	for _, o := range st.Overrides {
		if o == name {
			found = true
			break
		}
	}
	if !found {
		st.Overrides = append(st.Overrides, name)
		_ = config.SaveSettings(st)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": name})
}

func (s *Server) handleDeleteOverride(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	next := st.Overrides[:0]
	found := false
	for _, o := range st.Overrides {
		if o == name {
			found = true
			continue
		}
		next = append(next, o)
	}
	if !found {
		writeErr(w, 404, "not found")
		return
	}
	st.Overrides = next
	_ = config.SaveSettings(st)
	_ = os.Remove(filepath.Join(config.OverrideDir(), name))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func sanitizeName(name string) string {
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}
	return name
}

func (s *Server) handleProxies(w http.ResponseWriter, r *http.Request) {
	if !service.IsRunning() {
		writeErr(w, http.StatusServiceUnavailable, "内核未运行，请先启动")
		return
	}
	c, err := client.New()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	proxies, err := c.Proxies()
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, proxies)
}

func (s *Server) handleSelectProxy(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, 400, "name required")
		return
	}
	c, err := client.New()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := c.SelectProxy(group, body.Name); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "group": group, "name": body.Name})
}

func (s *Server) handleDelay(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := config.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	c, err := client.New()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	isGroup := false
	if proxies, err := c.Proxies(); err == nil {
		if pm, _ := proxies["proxies"].(map[string]any); pm != nil {
			if info, _ := pm[name].(map[string]any); info != nil {
				if _, ok := info["all"]; ok {
					isGroup = true
				}
			}
		}
	}
	var out map[string]any
	if isGroup {
		out, err = c.GroupDelay(name, st.DelayTestURL, st.DelayTestTimeout)
	} else {
		out, err = c.ProxyDelay(name, st.DelayTestURL, st.DelayTestTimeout)
	}
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	path, err := config.GenerateAndValidate()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "config": path})
}

func (s *Server) handleTrafficSSE(w http.ResponseWriter, r *http.Request) {
	s.proxyControllerStream(w, r, "/traffic")
}

func (s *Server) handleLogsSSE(w http.ResponseWriter, r *http.Request) {
	s.proxyControllerStream(w, r, "/logs")
}

func (s *Server) proxyControllerStream(w http.ResponseWriter, r *http.Request, path string) {
	c, err := client.New()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	resp, err := c.OpenStream(path)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	defer resp.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher.Flush()

	reader := bufio.NewReader(resp.Body)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			payload := strings.TrimSpace(string(line))
			if payload != "" {
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
