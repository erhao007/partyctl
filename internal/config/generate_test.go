package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestForceBaseSettings(t *testing.T) {
	profile := map[string]any{
		"mixed-port": 1111,
		"proxies":    []any{},
	}
	s := Settings{
		MixedPort:          7890,
		ExternalController: "127.0.0.1:9090",
		Secret:             "abc",
		Mode:               "rule",
		TUNEnabled:         true,
		LogLevel:           "info",
	}
	ForceBaseSettings(profile, s)
	if profile["mixed-port"] != 7890 {
		t.Fatalf("mixed-port: %#v", profile["mixed-port"])
	}
	if profile["secret"] != "abc" {
		t.Fatalf("secret: %#v", profile["secret"])
	}
	tun := profile["tun"].(map[string]any)
	if tun["enable"] != true {
		t.Fatalf("tun.enable: %#v", tun["enable"])
	}
}

func TestGeneratePipeline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PARTYCTL_MIRROR", "https://mirror.test.example")

	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	s.Secret = "testsecret"
	s.CurrentProfile = "local"
	s.Overrides = []string{"extra.yaml"}
	s.TUNEnabled = false
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}

	profile := map[string]any{
		"proxies": []any{
			map[string]any{"name": "DIRECT-NODE", "type": "direct"},
		},
		"proxy-groups": []any{
			map[string]any{
				"name":    "PROXY",
				"type":    "select",
				"proxies": []any{"DIRECT", "DIRECT-NODE"},
			},
		},
		"rules": []any{"MATCH,PROXY"},
	}
	raw, _ := yaml.Marshal(profile)
	if err := os.WriteFile(ProfilePath("local"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	ov := map[string]any{
		"+rules": []any{"DOMAIN-SUFFIX,forced.example,DIRECT"},
	}
	ovRaw, _ := yaml.Marshal(ov)
	if err := os.WriteFile(filepath.Join(OverrideDir(), "extra.yaml"), ovRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["mixed-port"] != 7890 {
		t.Fatalf("mixed-port forced: %#v", out["mixed-port"])
	}
	if out["secret"] != "testsecret" {
		t.Fatalf("secret: %#v", out["secret"])
	}
	rules, ok := out["rules"].([]any)
	if !ok || len(rules) < 2 {
		t.Fatalf("rules after override: %#v", out["rules"])
	}
	if rules[0] != "DOMAIN-SUFFIX,forced.example,DIRECT" {
		t.Fatalf("prepended rule missing: %#v", rules)
	}
	geox, ok := out["geox-url"].(map[string]any)
	if !ok {
		t.Fatalf("geox-url: %#v", out["geox-url"])
	}
	if geox["geoip"] != "https://mirror.test.example/geox/download/latest/geoip.dat" {
		t.Fatalf("geoip: %#v", geox["geoip"])
	}
	if geox["geosite"] != "https://mirror.test.example/geox/download/latest/geosite.dat" {
		t.Fatalf("geosite: %#v", geox["geosite"])
	}
}

func TestGenerateKeepsProfileGeoxAndCanDisableMirror(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PARTYCTL_MIRROR", "https://mirror.test.example")
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	s.Secret = "testsecret"
	s.CurrentProfile = "local"
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{
		"geox-url": map[string]any{"geoip": "https://example.test/geoip.dat"},
		"rules":    []any{"MATCH,DIRECT"},
	}
	raw, _ := yaml.Marshal(profile)
	if err := os.WriteFile(ProfilePath("local"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	geox := out["geox-url"].(map[string]any)
	if geox["geoip"] != "https://example.test/geoip.dat" {
		t.Fatalf("profile geox replaced: %#v", geox)
	}

	t.Setenv("PARTYCTL_MIRROR", "off")
	s.CurrentProfile = "plain"
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	plain := map[string]any{"rules": []any{"MATCH,DIRECT"}}
	raw, _ = yaml.Marshal(plain)
	if err := os.WriteFile(ProfilePath("plain"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	path, err = Generate()
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out = map[string]any{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["geox-url"]; ok {
		t.Fatalf("mirror off still injected geox: %#v", out["geox-url"])
	}
}
