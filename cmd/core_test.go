package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
)

func TestCoreInstallIfMissingSkipsDownload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	bin := config.CoreBinaryPath()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'Mihomo Meta fake'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out := withCmdOutput(t, []string{"core", "install", "--if-missing"})
	if !strings.Contains(out, "跳过下载") || !strings.Contains(out, "Mihomo Meta fake") {
		t.Fatalf("output:\n%s", out)
	}
	if !strings.Contains(out, "partyctl core install") {
		t.Fatalf("missing update hint:\n%s", out)
	}
}
