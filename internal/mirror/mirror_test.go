package mirror

import "testing"

func TestBase(t *testing.T) {
	t.Setenv(Env, "")
	// 空环境变量在 LookupEnv 里是“已设置”。用未设置的情况测默认值需要另开子测试前还原。
	if got := Normalize(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := Normalize(" off "); got != "" {
		t.Fatalf("off: %q", got)
	}
	if got := Normalize("https://mirror.example.com/"); got != "https://mirror.example.com" {
		t.Fatalf("trim: %q", got)
	}
	urls := GeoxURLs("https://mirror.example.com")
	if urls["geoip"] != "https://mirror.example.com/geox/download/latest/geoip.dat" {
		t.Fatalf("geoip: %v", urls)
	}
	if GeoxURLs("") != nil {
		t.Fatal("disabled mirror should not invent geox urls")
	}
}

func TestBaseDefault(t *testing.T) {
	t.Setenv(Env, DefaultBase)
	if Base() != DefaultBase {
		t.Fatalf("base: %s", Base())
	}
	t.Setenv(Env, "github")
	if Base() != "" {
		t.Fatalf("github mode: %q", Base())
	}
}
