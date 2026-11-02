package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
)

func withCmdOutput(t *testing.T, args []string) string {
	t.Helper()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestWebTokenAndURL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	st := config.DefaultSettings()
	st.Secret = "controller-secret"
	st.Web.Token = "web-tok"
	st.Web.Listen = "127.0.0.1:9800"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(withCmdOutput(t, []string{"web", "token"})); got != "web-tok" {
		t.Fatalf("token = %q", got)
	}
	if got := strings.TrimSpace(withCmdOutput(t, []string{"web", "url"})); got != "http://127.0.0.1:9800" {
		t.Fatalf("url = %q", got)
	}
}

func TestStatusCommandFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	st := config.DefaultSettings()
	st.Secret = "abc"
	st.CurrentProfile = "local"
	st.Mode = "rule"
	st.TUNEnabled = false
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	out := withCmdOutput(t, []string{"status"})
	for _, want := range []string{
		"核心: 未运行\n",
		"运行方式: -\n",
		"当前配置: local\n",
		"模式: rule\n",
		"mixed-port: 7890\n",
		"控制器: 127.0.0.1:9090\n",
		"TUN: 关\n",
		"Web: 未运行\n",
		"Web 运行方式: -\n",
		"Web 地址: http://127.0.0.1:9800\n",
		"config.yaml",
		"profiles/local.yaml",
		"work/config.yaml",
		"mihomo",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}
