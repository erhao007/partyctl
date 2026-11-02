package cmd

import (
	"strings"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
)

func TestPortSetUpdatesSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := config.DefaultSettings()
	st.Secret = "secret"
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	out := withCmdOutput(t, []string{"port", "set", "--mixed", "7891", "--controller", "127.0.0.1:9091"})
	if !strings.Contains(out, "mixed-port: 7891") || !strings.Contains(out, "external-controller: 127.0.0.1:9091") {
		t.Fatalf("output:\n%s", out)
	}
	if !strings.Contains(out, "partyctl restart") {
		t.Fatal(out)
	}
	got, err := config.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.MixedPort != 7891 || got.ExternalController != "127.0.0.1:9091" {
		t.Fatalf("%#v", got)
	}
}
