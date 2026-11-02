package diag

import "testing"

func TestAdvise(t *testing.T) {
	if got := Advise(Attempt{OK: true, Status: 200}, Attempt{OK: true, Status: 200}, true); got == "" {
		t.Fatal("empty")
	}
	msg := Advise(Attempt{OK: true, Status: 200}, Attempt{Error: "i/o timeout"}, true)
	if msg == "" || !contains(msg, "代理失败") {
		t.Fatalf("proxy fail advice: %s", msg)
	}
	off := Advise(Attempt{OK: true}, Attempt{}, false)
	if !contains(off, "内核未运行") {
		t.Fatalf("core down: %s", off)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (stringIndex(s, sub) >= 0)))
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
