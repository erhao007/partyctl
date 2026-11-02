package service

import (
	"strconv"
	"strings"
	"testing"
)

func TestChooseWebMode(t *testing.T) {
	if got := chooseWebMode(0, true, true); got != webModeSystem {
		t.Fatalf("root with systemd: %s", got)
	}
	if got := chooseWebMode(0, false, true); got != webModeUser {
		t.Fatalf("root without system bus: %s", got)
	}
	if got := chooseWebMode(1000, true, true); got != webModeUser {
		t.Fatalf("user with both: %s", got)
	}
	if got := chooseWebMode(1000, true, false); got != webModeDetach {
		t.Fatalf("user without user systemd: %s", got)
	}
	if got := chooseWebMode(0, false, false); got != webModeDetach {
		t.Fatalf("no systemd: %s", got)
	}
}

func TestWebUnitContent(t *testing.T) {
	spec := webUnitSpec{Exe: "/usr/local/bin/partyctl", Listen: "127.0.0.1:9800", Home: "/root"}
	system := webUnitContent(webModeSystem, spec)
	for _, want := range []string{
		"ExecStart=/usr/local/bin/partyctl web --listen 127.0.0.1:9800\n",
		"Environment=HOME=/root\n",
		"User=root\n",
		"WantedBy=multi-user.target\n",
		"Restart=on-failure\n",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system unit missing %q\n%s", want, system)
		}
	}
	user := webUnitContent(webModeUser, webUnitSpec{
		Exe: "/home/u/My Bin/partyctl", Listen: "127.0.0.1:9800", Home: "/home/u",
	})
	if strings.Contains(user, "User=root") {
		t.Fatal("user unit must not force User=root")
	}
	if !strings.Contains(user, "WantedBy=default.target\n") {
		t.Fatalf("user unit: %s", user)
	}
	if !strings.Contains(user, `ExecStart="/home/u/My Bin/partyctl" web --listen 127.0.0.1:9800`) {
		t.Fatalf("quoted exec: %s", user)
	}
}

func TestQuoteSystemd(t *testing.T) {
	if quoteSystemd("127.0.0.1:9800") != "127.0.0.1:9800" {
		t.Fatal("plain")
	}
	if quoteSystemd(`/opt/my bin/partyctl`) != `"/opt/my bin/partyctl"` {
		t.Fatal("spaces")
	}
}

func TestDialAddr(t *testing.T) {
	if dialAddr("0.0.0.0:9800") != "127.0.0.1:9800" {
		t.Fatal("wildcard")
	}
	if dialAddr("127.0.0.1:9800") != "127.0.0.1:9800" {
		t.Fatal("loopback")
	}
}

func TestWebReadyMessage(t *testing.T) {
	system := webReadyMessage(webModeSystem, "http://127.0.0.1:9800", "")
	if !strings.Contains(system, "开机自启") || !strings.Contains(system, "http://127.0.0.1:9800") {
		t.Fatal(system)
	}
	user := webReadyMessage(webModeUser, "http://127.0.0.1:9800", "")
	if !strings.Contains(user, "登录自启") {
		t.Fatal(user)
	}
	detached := webReadyMessage(webModeDetach, "http://127.0.0.1:9800", "")
	if !strings.Contains(detached, "没有可用的 systemd") || !strings.Contains(detached, "web enable") {
		t.Fatal(detached)
	}
	fallback := webReadyMessage(webModeDetach, "http://127.0.0.1:9800", "permission denied")
	if !strings.Contains(fallback, "permission denied") {
		t.Fatal(fallback)
	}
}

type fakeWebBackend struct {
	mode      string
	active    bool
	pid       int
	hasPID    bool
	mainPID   int
	mainOK    bool
	calls     []string
	content   string
	startErr  error
	readyErr  error
	detachErr error
}

func (f *fakeWebBackend) call(name string) { f.calls = append(f.calls, name) }

func (f *fakeWebBackend) DetectMode() string { return f.mode }
func (f *fakeWebBackend) InstallUnit(mode, content string) error {
	f.call("install:" + mode)
	f.content = content
	return nil
}
func (f *fakeWebBackend) Reload(mode string) error { f.call("reload"); return nil }
func (f *fakeWebBackend) Enable(mode string) error { f.call("enable"); return nil }
func (f *fakeWebBackend) IsActive(mode string) bool {
	f.call("active")
	return f.active
}
func (f *fakeWebBackend) Restart(mode string) error { f.call("restart"); return f.startErr }
func (f *fakeWebBackend) Start(mode string) error   { f.call("start"); return f.startErr }
func (f *fakeWebBackend) DisableNow(mode string) error {
	f.call("disable")
	return nil
}
func (f *fakeWebBackend) WebPID() (int, bool) { return f.pid, f.hasPID }
func (f *fakeWebBackend) UnitMainPID(mode string) (int, bool) {
	return f.mainPID, f.mainOK
}
func (f *fakeWebBackend) StopPID(pid int) error {
	f.call("stop:" + strconv.Itoa(pid))
	f.hasPID = false
	return nil
}
func (f *fakeWebBackend) StartDetached(listen string) error {
	f.call("detach:" + listen)
	return f.detachErr
}
func (f *fakeWebBackend) WaitReady(listen string) error {
	f.call("wait")
	return f.readyErr
}

func TestEnableWebSystemStartsAndRestarts(t *testing.T) {
	spec := webUnitSpec{Exe: "/usr/local/bin/partyctl", Listen: "127.0.0.1:9800", Home: "/root"}
	fresh := &fakeWebBackend{mode: webModeSystem}
	res, err := enableWebWith(fresh, spec, "http://127.0.0.1:9800")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != webModeSystem {
		t.Fatalf("mode %s", res.Mode)
	}
	joined := strings.Join(fresh.calls, ",")
	for _, step := range []string{"install:system", "reload", "enable", "active", "start", "wait"} {
		if !strings.Contains(joined, step) {
			t.Fatalf("missing %s in %s", step, joined)
		}
	}
	if strings.Contains(joined, "detach:") {
		t.Fatalf("should not detach: %s", joined)
	}
	if !strings.Contains(fresh.content, "WantedBy=multi-user.target") {
		t.Fatal(fresh.content)
	}

	again := &fakeWebBackend{mode: webModeSystem, active: true, pid: 42, hasPID: true, mainPID: 42, mainOK: true}
	if _, err := enableWebWith(again, spec, "http://127.0.0.1:9800"); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(again.calls, ",")
	if strings.Contains(joined, "stop:") {
		t.Fatalf("must not kill the systemd main pid: %s", joined)
	}
	if !strings.Contains(joined, "restart") {
		t.Fatalf("upgrade should restart: %s", joined)
	}
}

func TestEnableWebStopsStrayBeforeSystemd(t *testing.T) {
	b := &fakeWebBackend{mode: webModeUser, pid: 7, hasPID: true}
	spec := webUnitSpec{Exe: "/bin/partyctl", Listen: "127.0.0.1:9800", Home: "/home/u"}
	if _, err := enableWebWith(b, spec, "http://127.0.0.1:9800"); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) == 0 || !strings.HasPrefix(b.calls[0], "stop:7") {
		t.Fatalf("calls: %v", b.calls)
	}
	if !strings.Contains(b.content, "WantedBy=default.target") {
		t.Fatal(b.content)
	}
}

func TestEnableWebFallsBackToDetach(t *testing.T) {
	b := &fakeWebBackend{mode: webModeSystem, startErr: errString("boom")}
	spec := webUnitSpec{Exe: "/bin/partyctl", Listen: "127.0.0.1:9800", Home: "/root"}
	res, err := enableWebWith(b, spec, "http://127.0.0.1:9800")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != webModeDetach {
		t.Fatalf("mode %s", res.Mode)
	}
	joined := strings.Join(b.calls, ",")
	if !strings.Contains(joined, "disable") || !strings.Contains(joined, "detach:127.0.0.1:9800") {
		t.Fatalf("calls: %s", joined)
	}
	if !strings.Contains(res.Message, "boom") {
		t.Fatal(res.Message)
	}
}

func TestEnableWebDetachRestartsExisting(t *testing.T) {
	b := &fakeWebBackend{mode: webModeDetach, pid: 9, hasPID: true}
	spec := webUnitSpec{Exe: "/bin/partyctl", Listen: "127.0.0.1:9800", Home: "/home/u"}
	res, err := enableWebWith(b, spec, "http://127.0.0.1:9800")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != webModeDetach {
		t.Fatal(res.Mode)
	}
	joined := strings.Join(b.calls, ",")
	if joined != "stop:9,detach:127.0.0.1:9800" {
		t.Fatalf("calls: %s", joined)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
