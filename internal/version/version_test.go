package version

import "testing"

func TestStringDefault(t *testing.T) {
	if got := String(); got != "dev" {
		t.Fatalf("version = %q, want dev", got)
	}
}
