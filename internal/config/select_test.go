package config

import "testing"

func TestEnsureSelectionKeepsExistingSelect(t *testing.T) {
	profile := map[string]any{
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"DIRECT"}},
		},
		"rules": []any{"MATCH,PROXY"},
	}
	ensureSelection(profile)
	prof := profile["profile"].(map[string]any)
	if prof["store-selected"] != true {
		t.Fatalf("store-selected %#v", prof["store-selected"])
	}
	groups := profile["proxy-groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("should not inject another group: %#v", groups)
	}
	if profile["rules"].([]any)[0] != "MATCH,PROXY" {
		t.Fatalf("rules changed: %#v", profile["rules"])
	}
}

func TestEnsureSelectionWrapsSingleURLTest(t *testing.T) {
	profile := map[string]any{
		"proxies": []any{
			map[string]any{"name": "HK", "type": "ss"},
		},
		"proxy-groups": []any{
			map[string]any{"name": "自动选择", "type": "url-test", "proxies": []any{"HK"}},
		},
		"rules": []any{"MATCH,自动选择", "DOMAIN,example.com,DIRECT"},
	}
	ensureSelection(profile)
	groups := profile["proxy-groups"].([]any)
	manual := groups[0].(map[string]any)
	if manual["name"] != "手动选择" || manual["type"] != "select" {
		t.Fatalf("manual %#v", manual)
	}
	opts := manual["proxies"].([]any)
	if opts[0] != "自动选择" {
		t.Fatalf("default option should stay the auto group: %#v", opts)
	}
	rules := profile["rules"].([]any)
	if rules[0] != "MATCH,手动选择" {
		t.Fatalf("rule not retargeted: %#v", rules)
	}
	if rules[1] != "DOMAIN,example.com,DIRECT" {
		t.Fatalf("direct rule changed: %#v", rules[1])
	}
}
