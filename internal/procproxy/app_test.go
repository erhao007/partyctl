package procproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchSelfByComm(t *testing.T) {
	comm := readComm(os.Getpid())
	if comm == "" {
		t.Fatal("no comm")
	}
	if _, ok := matchOne(App{Match: comm}, os.Getpid()); !ok {
		t.Fatalf("comm %q did not match", comm)
	}
	if _, ok := matchOne(App{Match: comm + "-not"}, os.Getpid()); ok {
		t.Fatal("unexpected match")
	}
}

func TestMatchSelfByPath(t *testing.T) {
	exe := procExe(os.Getpid())
	if exe == "" {
		t.Skip("no exe")
	}
	if _, ok := matchOne(App{Match: exe, Path: exe}, os.Getpid()); !ok {
		t.Fatalf("path %s did not match", exe)
	}
	other := filepath.Join(filepath.Dir(exe), "not-this-binary")
	if _, ok := matchOne(App{Match: other, Path: other}, os.Getpid()); ok {
		t.Fatal("other path matched")
	}
}

func TestAddAppPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app, err := AddApp("firefox")
	if err != nil {
		t.Fatal(err)
	}
	if app.Match != "firefox" {
		t.Fatalf("%#v", app)
	}
	snap := Snapshot()
	if len(snap.Apps) != 1 || !snap.Enabled {
		t.Fatalf("%#v", snap)
	}
	if !HasApps() {
		t.Fatal("HasApps")
	}
}
