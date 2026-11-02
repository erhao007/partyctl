package service

import (
	"strings"
	"testing"
)

func TestFormatStatusRunning(t *testing.T) {
	got := FormatStatus(Report{
		CoreRunning:  true,
		CorePID:      42,
		RunMode:      "systemd",
		CoreVersion:  "Mihomo Meta v1",
		Profile:      "mysub",
		Mode:         "rule",
		MixedPort:    7890,
		Controller:   "127.0.0.1:9090",
		TUN:          true,
		WebRunning:   true,
		WebMode:      webModeSystem,
		WebURL:       "http://127.0.0.1:9800",
		SettingsPath: "/tmp/config.yaml",
		ProfilePath:  "/tmp/profiles/mysub.yaml",
		WorkConfig:   "/tmp/work/config.yaml",
		CoreBinary:   "/tmp/mihomo",
	})
	want := strings.Join([]string{
		"核心: 运行中",
		"pid: 42",
		"运行方式: systemd",
		"内核版本: Mihomo Meta v1",
		"当前配置: mysub",
		"模式: rule",
		"mixed-port: 7890",
		"控制器: 127.0.0.1:9090",
		"TUN: 开",
		"Web: 运行中",
		"Web 运行方式: systemd（开机自启）",
		"Web 地址: http://127.0.0.1:9800",
		"Web 局域网: 关",
		"设置: /tmp/config.yaml",
		"当前配置文件: /tmp/profiles/mysub.yaml",
		"运行配置: /tmp/work/config.yaml",
		"内核: /tmp/mihomo",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("format:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatStatusStopped(t *testing.T) {
	got := FormatStatus(Report{
		CoreVersion:  "未安装",
		Mode:         "rule",
		MixedPort:    7890,
		Controller:   "127.0.0.1:9090",
		WebURL:       "http://127.0.0.1:9800",
		SettingsPath: "/tmp/config.yaml",
		WorkConfig:   "/tmp/work/config.yaml",
		CoreBinary:   "/tmp/mihomo",
	})
	for _, line := range []string{
		"核心: 未运行",
		"pid: -",
		"运行方式: -",
		"内核版本: 未安装",
		"当前配置: -",
		"模式: rule",
		"mixed-port: 7890",
		"控制器: 127.0.0.1:9090",
		"TUN: 关",
		"Web: 未运行",
		"Web 运行方式: -",
		"Web 地址: http://127.0.0.1:9800",
		"Web 局域网: 关",
		"当前配置文件: -",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("missing %q in\n%s", line, got)
		}
	}
}

func TestArgMatchers(t *testing.T) {
	work := "/home/u/.config/partyctl/work"
	bin := "/home/u/.local/share/partyctl/mihomo"
	if !isCoreArgs([]string{bin, "-d", work}, bin, work) {
		t.Fatal("expected core match")
	}
	if isCoreArgs([]string{"partyctl", "status"}, bin, work) {
		t.Fatal("status is not the core")
	}
	if !isWebServerArgs([]string{"/usr/local/bin/partyctl", "web", "--listen", "127.0.0.1:9800"}) {
		t.Fatal("expected web server")
	}
	if isWebServerArgs([]string{"partyctl", "web", "token"}) {
		t.Fatal("web token is not the server")
	}
	if isWebServerArgs([]string{"partyctl", "web", "enable"}) {
		t.Fatal("web enable is not the server")
	}
	if isWebServerArgs([]string{"partyctl", "web", "lan", "on"}) {
		t.Fatal("web lan is not the server")
	}
	if !isWebServerArgs([]string{"/usr/local/bin/partyctl", "web", "--listen=127.0.0.1:9800"}) {
		t.Fatal("expected --listen= form to count as the server")
	}
	if isWebServerArgs([]string{"partyctl", "status"}) {
		t.Fatal("status is not web")
	}
}
