package service

import (
	"strings"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
)

func TestSetWebLANKeepsPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := config.DefaultSettings()
	st.Secret = "secret"
	st.Web.Listen = "127.0.0.1:9801"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	got, err := setWebLAN(true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Web.Listen != "0.0.0.0:9801" {
		t.Fatalf("on: %s", got.Web.Listen)
	}
	if got.Secret != "secret" {
		t.Fatal("secret changed")
	}
	got, err = setWebLAN(false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Web.Listen != "127.0.0.1:9801" {
		t.Fatalf("off: %s", got.Web.Listen)
	}
}

func TestWebLANRestartsAndPrints(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := config.DefaultSettings()
	st.Secret = "secret"
	st.Web.Listen = "127.0.0.1:9800"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	old := restartWebForLAN
	restartWebForLAN = func() (WebEnableResult, error) {
		return WebEnableResult{Message: "Web 已在后台启动。\n地址: http://0.0.0.0:9800\n"}, nil
	}
	t.Cleanup(func() { restartWebForLAN = old })

	msg, err := WebLAN(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"已开启局域网访问，Web 监听 0.0.0.0:9800",
		"只在可信网络上使用。",
		"http://127.0.0.1:9800",
		"登录令牌: partyctl web token",
		"Web 已在后台启动。",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q\n%s", want, msg)
		}
	}
	saved, err := config.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Web.Listen != "0.0.0.0:9800" {
		t.Fatalf("saved %s", saved.Web.Listen)
	}
}

func TestFormatWebLANAndUFW(t *testing.T) {
	msg := formatWebLAN(true, "0.0.0.0", "9800", []string{"10.1.2.3", "2001:db8::1"}, ufwHintFromStatus(true, "Status: active", "9800"), "")
	if !strings.Contains(msg, "http://10.1.2.3:9800") || !strings.Contains(msg, "http://[2001:db8::1]:9800") {
		t.Fatal(msg)
	}
	if !strings.Contains(msg, "sudo ufw allow 9800/tcp") || !strings.Contains(msg, "不会自动修改防火墙") {
		t.Fatal(msg)
	}
	if hint := ufwHintFromStatus(true, "Status: inactive", "9800"); hint != "" {
		t.Fatal(hint)
	}
	if hint := ufwHintFromStatus(true, "状态：不活动", "9800"); hint != "" {
		t.Fatal(hint)
	}
	if hint := ufwHintFromStatus(false, "Status: active", "9800"); hint != "" {
		t.Fatal(hint)
	}
	off := formatWebLAN(false, "127.0.0.1", "9800", []string{"10.1.2.3"}, "should not show", "")
	if strings.Contains(off, "ufw") || strings.Contains(off, "10.1.2.3") {
		t.Fatal(off)
	}
	if !strings.Contains(off, "已关闭局域网访问") {
		t.Fatal(off)
	}
}

func TestWebLANOpen(t *testing.T) {
	if WebLANOpen("127.0.0.1:9800") || WebLANOpen("[::1]:9800") {
		t.Fatal("loopback should be closed")
	}
	if !WebLANOpen("0.0.0.0:9800") || !WebLANOpen("192.168.1.2:9800") {
		t.Fatal("lan should be open")
	}
}
