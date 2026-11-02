package merge

import (
	"reflect"
	"testing"
)

func TestDeepMergeReplaceBang(t *testing.T) {
	target := map[string]any{
		"dns": map[string]any{"enable": true, "nameserver": []any{"1.1.1.1"}},
	}
	other := map[string]any{
		"dns!": map[string]any{"enable": false},
	}
	DeepMerge(target, other)
	dns, ok := target["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns missing: %#v", target)
	}
	if dns["enable"] != false {
		t.Fatalf("expected enable=false, got %#v", dns)
	}
	if _, exists := dns["nameserver"]; exists {
		t.Fatalf("nameserver should be replaced away: %#v", dns)
	}
}

func TestDeepMergeNested(t *testing.T) {
	target := map[string]any{
		"tun": map[string]any{"enable": false, "stack": "system"},
	}
	other := map[string]any{
		"tun": map[string]any{"enable": true},
	}
	DeepMerge(target, other)
	tun := target["tun"].(map[string]any)
	if tun["enable"] != true {
		t.Fatalf("enable: %#v", tun["enable"])
	}
	if tun["stack"] != "system" {
		t.Fatalf("stack should remain: %#v", tun["stack"])
	}
}

func TestDeepMergePrependAppend(t *testing.T) {
	target := map[string]any{
		"rules": []any{"DOMAIN-SUFFIX,example.com,DIRECT"},
	}
	DeepMerge(target, map[string]any{"+rules": []any{"DOMAIN,pre.com,PROXY"}})
	got := asSlice(target["rules"])
	want := []any{"DOMAIN,pre.com,PROXY", "DOMAIN-SUFFIX,example.com,DIRECT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prepend: got %#v want %#v", got, want)
	}
	DeepMerge(target, map[string]any{"rules+": []any{"MATCH,DIRECT"}})
	got = asSlice(target["rules"])
	want = []any{"DOMAIN,pre.com,PROXY", "DOMAIN-SUFFIX,example.com,DIRECT", "MATCH,DIRECT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("append: got %#v want %#v", got, want)
	}
}

func TestDeepMergeArrayReplace(t *testing.T) {
	target := map[string]any{"proxies": []any{"a"}}
	DeepMerge(target, map[string]any{"proxies": []any{"b", "c"}})
	got := asSlice(target["proxies"])
	want := []any{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestDeepMergeTrimWrap(t *testing.T) {
	target := map[string]any{}
	DeepMerge(target, map[string]any{"<dns>!": map[string]any{"enable": true}})
	if _, ok := target["dns"].(map[string]any); !ok {
		t.Fatalf("expected dns key, got %#v", target)
	}
}

func TestCloneMap(t *testing.T) {
	src := map[string]any{"a": map[string]any{"b": 1}, "c": []any{2}}
	cp := CloneMap(src)
	cp["a"].(map[string]any)["b"] = 99
	if src["a"].(map[string]any)["b"] != 1 {
		t.Fatal("clone mutated source")
	}
}
