package procproxy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchMovesMatchingProcess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("需要 root")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	src, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "partyctl-e2e-bin")
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AddApp(bin); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ServeWatch(ctx, func(pid int) bool { return pid == os.Getpid() })
	time.Sleep(700 * time.Millisecond)
	cmd := exec.Command(bin, "3")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_, _ = Clear()
		_ = Disarm()
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cg, _ := ReadCgroup(cmd.Process.Pid)
		if cg == "/"+CgroupName {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	cg, _ := ReadCgroup(cmd.Process.Pid)
	t.Fatalf("process stayed in %s", cg)
}
