package service

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

func TestIsOurCore(t *testing.T) {
	bin := "/home/u/.local/share/partyctl/mihomo"
	work := "/home/u/.config/partyctl/work"
	if !isOurCore(bin, work, []string{bin, "-d", work}, bin, work) {
		t.Fatal("exe and -d")
	}
	if !isOurCore(bin+" (deleted)", "/tmp", []string{bin, "-d", work}, bin, work) {
		t.Fatal("replaced binary should still match")
	}
	if !isOurCore("/bin/sh", work, []string{"/bin/sh", bin, "-d", work}, bin, work) {
		t.Fatal("shebang launcher in the work dir")
	}
	if isOurCore("/usr/bin/mihomo", "/etc/mihomo", []string{"/usr/bin/mihomo", "-d", "/etc/mihomo"}, bin, work) {
		t.Fatal("foreign mihomo must be ignored")
	}
	if isOurCore("/usr/bin/partyctl", work, []string{"partyctl", "status"}, bin, work) {
		t.Fatal("partyctl status is not the core")
	}
}

func TestSingleInstanceStartStop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PARTYCTL_FORCE_DETACH", "1")
	t.Setenv("PARTYCTL_MIRROR", "off")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	bin := config.CoreBinaryPath()
	script := "#!/bin/sh\nif [ \"$1\" = \"-t\" ] || [ \"$1\" = \"-v\" ]; then echo fake; exit 0; fi\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	st := config.DefaultSettings()
	st.Secret = "secret"
	st.CurrentProfile = "local"
	st.MixedPort = freePort(t)
	st.ExternalController = fmt.Sprintf("127.0.0.1:%d", freePort(t))
	if err := config.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	profile := []byte("proxies:\n  - {name: node-a, type: direct}\nrules:\n  - MATCH,DIRECT\n")
	if err := os.WriteFile(config.ProfilePath("local"), profile, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })

	if err := StartDetached(); err != nil {
		t.Fatal(err)
	}
	waitCores(t, 1)
	err := StartDetached()
	if !IsAlreadyRunning(err) {
		t.Fatalf("second start: %v", err)
	}
	if got := ListOurCores(); len(got) != 1 {
		t.Fatalf("cores after second start: %#v", got)
	}
	// A leftover duplicate started outside start.
	extra := exec.Command(bin, "-d", config.WorkDir())
	extra.Stdout, extra.Stderr = nil, nil
	if err := extra.Start(); err != nil {
		t.Fatal(err)
	}
	waitCores(t, 2)
	msg, err := ReconcileCore()
	if err != nil {
		t.Fatal(err, msg)
	}
	waitCores(t, 1)
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	waitCores(t, 0)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitCores(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(ListOurCores()) == n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("want %d cores, got %d %#v", n, len(ListOurCores()), ListOurCores())
}

func TestProfilePathDir(t *testing.T) {
	if filepath.Base(config.CoreBinaryPath()) != "mihomo" {
		t.Fatal(config.CoreBinaryPath())
	}
}
