package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/partyctl/partyctl/internal/merge"
	"github.com/partyctl/partyctl/internal/mirror"
	"github.com/partyctl/partyctl/internal/procproxy"
	"gopkg.in/yaml.v3"
)

// Generate builds work/config.yaml: profile → overrides → forced base settings.
func Generate() (string, error) {
	s, err := LoadSettings()
	if err != nil {
		return "", err
	}
	if s.CurrentProfile == "" {
		return "", fmt.Errorf("还没有订阅：请在 Web「订阅」页添加并启用，或执行 partyctl sub add / partyctl sub use")
	}
	profilePath := ProfilePath(s.CurrentProfile)
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return "", fmt.Errorf("read profile %s: %w", s.CurrentProfile, err)
	}
	var profile map[string]any
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		return "", fmt.Errorf("parse profile: %w", err)
	}
	if profile == nil {
		profile = map[string]any{}
	}
	profile = merge.CloneMap(profile)

	// Apply overrides in order
	for _, name := range s.Overrides {
		ovPath := filepath.Join(OverrideDir(), name)
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			ovPath = filepath.Join(OverrideDir(), name+".yaml")
		}
		ovData, err := os.ReadFile(ovPath)
		if err != nil {
			return "", fmt.Errorf("read override %s: %w", name, err)
		}
		var patch map[string]any
		if err := yaml.Unmarshal(ovData, &patch); err != nil {
			return "", fmt.Errorf("parse override %s: %w", name, err)
		}
		if patch != nil {
			profile = merge.DeepMerge(profile, patch)
		}
	}

	// Force base settings (last wins)
	ForceBaseSettings(profile, s)

	if err := EnsureDirs(); err != nil {
		return "", err
	}
	out, err := yaml.Marshal(profile)
	if err != nil {
		return "", err
	}
	workCfg := WorkConfigPath()
	if err := os.WriteFile(workCfg, out, 0o644); err != nil {
		return "", err
	}
	return workCfg, nil
}

// ForceBaseSettings applies partyctl-controlled fields onto the generated config.
func ForceBaseSettings(profile map[string]any, s Settings) {
	// mihomo 已删除该字段，留在运行配置里只会打出警告。
	delete(profile, "global-client-fingerprint")
	profile["mixed-port"] = s.MixedPort
	profile["external-controller"] = s.ExternalController
	profile["secret"] = s.Secret
	profile["mode"] = s.Mode
	profile["allow-lan"] = s.AllowLan
	if s.LogLevel != "" {
		profile["log-level"] = s.LogLevel
	}
	if s.TUNEnabled {
		tun, _ := profile["tun"].(map[string]any)
		if tun == nil {
			tun = map[string]any{}
		}
		tun["enable"] = true
		if _, ok := tun["stack"]; !ok {
			tun["stack"] = "system"
		}
		if _, ok := tun["auto-route"]; !ok {
			tun["auto-route"] = true
		}
		if _, ok := tun["auto-detect-interface"]; !ok {
			tun["auto-detect-interface"] = true
		}
		profile["tun"] = tun
	} else {
		if tun, ok := profile["tun"].(map[string]any); ok && tun != nil {
			tun["enable"] = false
			profile["tun"] = tun
		} else {
			profile["tun"] = map[string]any{"enable": false}
		}
	}
	applyMirrorGeox(profile)
	// 仅在按进程代理开启时补 tproxy / routing-mark / DNS，不改 tun 开关。
	procproxy.ApplyRuntime(profile)
}

// applyMirrorGeox 在订阅没有自带 geox-url 时，把 Geo 文件指到镜像，避免内核再去访问 GitHub。
func applyMirrorGeox(profile map[string]any) {
	if _, ok := profile["geox-url"]; ok {
		return
	}
	urls := mirror.GeoxURLs(mirror.Base())
	if urls == nil {
		return
	}
	raw := make(map[string]any, len(urls))
	for k, v := range urls {
		raw[k] = v
	}
	profile["geox-url"] = raw
}

// Validate runs `mihomo -t -d <workdir>`.
func Validate(coreBin string) error {
	if coreBin == "" {
		coreBin = CoreBinaryPath()
	}
	if _, err := os.Stat(coreBin); err != nil {
		return fmt.Errorf("未找到 mihomo 内核（%s）。请执行: partyctl core install", coreBin)
	}
	cmd := exec.Command(coreBin, "-t", "-d", WorkDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("config validation failed: %v\n%s", err, string(out))
	}
	return nil
}

// GenerateAndValidate generates then validates.
func GenerateAndValidate() (string, error) {
	path, err := Generate()
	if err != nil {
		return "", err
	}
	if err := Validate(""); err != nil {
		return path, err
	}
	return path, nil
}
