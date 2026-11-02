package merge

import (
	"strings"
)

// DeepMerge merges other into target following Clash Party semantics:
//   - key ending with "!" forces replace of that object
//   - "+key" prepends array values
//   - "key+" appends array values
//   - keys may be wrapped in <...>
//   - nested objects are recursively merged; arrays without +/- replace; scalars assign
func DeepMerge(target, other map[string]any) map[string]any {
	if target == nil {
		target = map[string]any{}
	}
	if other == nil {
		return target
	}
	for key, val := range other {
		applyKey(target, key, val)
	}
	return target
}

func trimWrap(s string) string {
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func asSlice(v any) []any {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []any:
		return s
	default:
		return nil
	}
}

func applyKey(target map[string]any, key string, val any) {
	if isObject(val) {
		if strings.HasSuffix(key, "!") {
			k := trimWrap(strings.TrimSuffix(key, "!"))
			target[k] = val
			return
		}
		k := trimWrap(key)
		existing, ok := target[k].(map[string]any)
		if !ok || existing == nil {
			existing = map[string]any{}
			target[k] = existing
		}
		DeepMerge(existing, val.(map[string]any))
		return
	}

	if asSlice(val) != nil {
		arr := asSlice(val)
		if strings.HasPrefix(key, "+") {
			k := trimWrap(key[1:])
			cur := asSlice(target[k])
			if cur == nil {
				cur = []any{}
			}
			target[k] = append(append([]any{}, arr...), cur...)
			return
		}
		if strings.HasSuffix(key, "+") {
			k := trimWrap(strings.TrimSuffix(key, "+"))
			cur := asSlice(target[k])
			if cur == nil {
				cur = []any{}
			}
			target[k] = append(append([]any{}, cur...), arr...)
			return
		}
		k := trimWrap(key)
		target[k] = arr
		return
	}

	// scalar / other: assign as-is (keep key wrapping trimmed for consistency with objects)
	k := trimWrap(key)
	// For plain scalars Clash Party uses Object.assign with original key;
	// trimWrap of non-wrapped key is identity.
	target[k] = val
}

// CloneMap deep-copies a YAML-ish map tree (maps and slices of any).
func CloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return CloneMap(t)
	case []any:
		cp := make([]any, len(t))
		for i, e := range t {
			cp[i] = cloneValue(e)
		}
		return cp
	default:
		return v
	}
}
