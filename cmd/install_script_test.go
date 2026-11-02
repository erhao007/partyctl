package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallScriptHelp(t *testing.T) {
	cmd := exec.Command("sh", filepath.Join(repoRoot(t), "install.sh"), "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"--no-start", "--user", "PARTYCTL_NO_START", "PARTYCTL_VERSION", "PARTYCTL_BASE_URL"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %s\n%s", want, text)
		}
	}
	bad := exec.Command("sh", filepath.Join(repoRoot(t), "install.sh"), "--not-a-flag")
	if err := bad.Run(); err == nil {
		t.Fatal("unknown flag should fail")
	}
	syntax := exec.Command("sh", "-n", filepath.Join(repoRoot(t), "install.sh"))
	if out, err := syntax.CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}

func TestInstallScriptSetUpAndIdempotent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installer requires linux")
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skip("unsupported arch")
	}
	asset := "partyctl-linux-" + arch + ".tar.gz"
	home := t.TempDir()
	logPath := filepath.Join(home, "fake.log")
	srv, _ := servePartyctlArchive(t, asset, fakePartyctlScript())

	marker := filepath.Join(home, ".config", "partyctl", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("preserve-me"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runInstall(t, home, logPath, srv.URL, nil, "--user")
	if !strings.Contains(out, "登录令牌: test-token") || !strings.Contains(out, "http://127.0.0.1:9800") {
		t.Fatalf("summary:\n%s", out)
	}
	if !strings.Contains(out, "ssh -L 9800:127.0.0.1:9800") || !strings.Contains(out, "订阅") {
		t.Fatalf("summary missing remote or subscription hint:\n%s", out)
	}
	if !strings.Contains(out, "web lan on") {
		t.Fatalf("summary should recommend web lan on:\n%s", out)
	}
	if !strings.Contains(out, "partyctl 已就绪") {
		t.Fatalf("expected success banner:\n%s", out)
	}
	kept, err := os.ReadFile(marker)
	if err != nil || string(kept) != "preserve-me" {
		t.Fatalf("config not preserved: %v %q", err, kept)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "partyctl")); err != nil {
		t.Fatal(err)
	}

	runInstall(t, home, logPath, srv.URL, nil, "--user")
	log := readLog(t, logPath)
	if strings.Count(log, "core install --if-missing") != 2 {
		t.Fatalf("core install calls:\n%s", log)
	}
	if strings.Count(log, "web enable") != 2 {
		t.Fatalf("web enable calls:\n%s", log)
	}
}

func TestInstallScriptContinuesAfterSetupFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installer requires linux")
	}
	arch := runtime.GOARCH
	asset := "partyctl-linux-" + arch + ".tar.gz"
	home := t.TempDir()
	logPath := filepath.Join(home, "fake.log")
	srv, _ := servePartyctlArchive(t, asset, fakePartyctlScript())

	out := runInstall(t, home, logPath, srv.URL, []string{"FAKE_CORE_FAIL=1"}, "--user")
	if !strings.Contains(out, "警告: 内核没有安装成功") {
		t.Fatalf("missing core warning:\n%s", out)
	}
	if !strings.Contains(out, "partyctl 已安装（有步骤未完成") {
		t.Fatalf("binary install should still finish:\n%s", out)
	}
	log := readLog(t, logPath)
	if !strings.Contains(log, "web enable") {
		t.Fatalf("web should still be attempted:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "partyctl")); err != nil {
		t.Fatal("binary missing", err)
	}

	home2 := t.TempDir()
	log2 := filepath.Join(home2, "fake.log")
	out = runInstall(t, home2, log2, srv.URL, []string{"FAKE_WEB_FAIL=1"}, "--user")
	if !strings.Contains(out, "警告: Web 管理界面没有启动成功") {
		t.Fatalf("missing web warning:\n%s", out)
	}
	if !strings.Contains(out, "登录令牌: test-token") {
		t.Fatalf("token should still be printed:\n%s", out)
	}
}

func TestInstallScriptNoStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installer requires linux")
	}
	arch := runtime.GOARCH
	asset := "partyctl-linux-" + arch + ".tar.gz"
	home := t.TempDir()
	logPath := filepath.Join(home, "fake.log")
	srv, _ := servePartyctlArchive(t, asset, fakePartyctlScript())

	out := runInstall(t, home, logPath, srv.URL, nil, "--user", "--no-start")
	log := readLog(t, logPath)
	if strings.Contains(log, "core install") || strings.Contains(log, "web enable") {
		t.Fatalf("--no-start still ran setup:\n%s", log)
	}
	if !strings.Contains(out, "已跳过内核安装和 Web 启动") {
		t.Fatalf("output:\n%s", out)
	}

	home2 := t.TempDir()
	log2 := filepath.Join(home2, "fake.log")
	out = runInstall(t, home2, log2, srv.URL, []string{"PARTYCTL_NO_START=yes"}, "--user")
	log = readLog(t, log2)
	if strings.Contains(log, "web enable") {
		t.Fatalf("PARTYCTL_NO_START still started web:\n%s", log)
	}
	if !strings.Contains(out, "已跳过") {
		t.Fatalf("output:\n%s", out)
	}
}

func runInstall(t *testing.T, home, logPath, base string, extraEnv []string, args ...string) string {
	t.Helper()
	scriptArgs := append([]string{filepath.Join(repoRoot(t), "install.sh")}, args...)
	cmd := exec.Command("sh", scriptArgs...)
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"PARTYCTL_BASE_URL=" + base,
		"PARTYCTL_FAKE_LOG=" + logPath,
		"PARTYCTL_VERSION=latest",
	}
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	return string(out)
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Dir(filepath.Dir(file))
}

func fakePartyctlScript() string {
	return `#!/bin/sh
log="${PARTYCTL_FAKE_LOG:?}"
printf '%s\n' "$*" >> "$log"
cmd=$1
sub=$2
case "$cmd" in
version)
	echo dev
	exit 0
	;;
core)
	if [ "$sub" = "version" ]; then
		echo fake
		exit 0
	fi
	if [ "${FAKE_CORE_FAIL:-}" = "1" ]; then
		echo "core failed" >&2
		exit 1
	fi
	exit 0
	;;
web)
	if [ "$sub" = "url" ]; then
		echo "http://127.0.0.1:9800"
		exit 0
	fi
	if [ "$sub" = "token" ]; then
		echo "test-token"
		exit 0
	fi
	if [ "${FAKE_WEB_FAIL:-}" = "1" ]; then
		echo "web failed" >&2
		exit 1
	fi
	exit 0
	;;
esac
exit 0
`
}

func servePartyctlArchive(t *testing.T, asset, script string) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, asset)
	writeGzipTar(t, tarPath, script)
	sum := sha256File(t, tarPath)
	sumPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumPath, []byte(sum+"  "+asset+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch path.Base(r.URL.Path) {
		case asset:
			http.ServeFile(w, r, tarPath)
		case "SHA256SUMS":
			http.ServeFile(w, r, sumPath)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, tarPath
}

func writeGzipTar(t *testing.T, dest, script string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte(script)
	hdr := &tar.Header{Name: "partyctl", Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
