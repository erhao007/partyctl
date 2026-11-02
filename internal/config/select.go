// 保证有一个可手动选择的策略组，并让 mihomo 把选择记到缓存里，重启后还能恢复。
package config

import "strings"

// ensureSelection 打开 store-selected。订阅里没有 select 组时补上「手动选择」。
// 只有一个自动组被规则引用时，把这些规则改指向「手动选择」，并把原自动组放在第一项，默认行为不变。
func ensureSelection(profile map[string]any) {
	prof, _ := profile["profile"].(map[string]any)
	if prof == nil {
		prof = map[string]any{}
	}
	prof["store-selected"] = true
	profile["profile"] = prof

	groups := anySlice(profile["proxy-groups"])
	if len(groups) == 0 || hasSelectGroup(groups) {
		return
	}
	auto := map[string]bool{}
	var autoOrder []string
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == "手动选择" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, g := range groups {
		m, _ := g.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		add(name)
		switch strings.ToLower(strings.TrimSpace(asString(m["type"]))) {
		case "url-test", "urltest", "fallback":
			if name != "" && !auto[name] {
				auto[name] = true
				autoOrder = append(autoOrder, name)
			}
		}
	}
	for _, p := range anySlice(profile["proxies"]) {
		m, _ := p.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		add(name)
	}
	add("DIRECT")
	var opts []any
	for _, name := range autoOrder {
		opts = append(opts, name)
	}
	for _, name := range names {
		if auto[name] {
			continue
		}
		opts = append(opts, name)
	}
	manual := map[string]any{
		"name":    "手动选择",
		"type":    "select",
		"proxies": opts,
	}
	profile["proxy-groups"] = append([]any{manual}, groups...)
	if len(autoOrder) == 1 {
		rewritePolicy(profile, autoOrder[0], "手动选择")
	}
}

func hasSelectGroup(groups []any) bool {
	for _, g := range groups {
		m, _ := g.(map[string]any)
		if m == nil {
			continue
		}
		if strings.EqualFold(asString(m["type"]), "select") {
			return true
		}
	}
	return false
}

func rewritePolicy(profile map[string]any, from, to string) {
	rules := anySlice(profile["rules"])
	if rules == nil {
		return
	}
	changed := false
	for i, rule := range rules {
		s, ok := rule.(string)
		if !ok {
			continue
		}
		parts := strings.Split(s, ",")
		for j := 1; j < len(parts); j++ {
			if strings.TrimSpace(parts[j]) == from {
				parts[j] = to
				changed = true
			}
		}
		rules[i] = strings.Join(parts, ",")
	}
	if changed {
		profile["rules"] = rules
	}
}

func anySlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	default:
		return nil
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
