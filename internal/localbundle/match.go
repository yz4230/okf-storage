package localbundle

import (
	"slices"
)

func match(target any, filter any) bool {
	switch t := target.(type) {
	case map[string]any:
		f, ok := filter.(map[string]any)
		if !ok {
			return false
		}
		for k, fv := range f {
			tv, ok := t[k]
			if !ok || !match(tv, fv) {
				return false
			}
		}
		return true
	case []any:
		f, ok := filter.([]any)
		if !ok {
			f = []any{filter}
		}
		for _, fv := range f {
			if !slices.ContainsFunc(t, func(tv any) bool { return match(tv, fv) }) {
				return false
			}
		}
		return true
	default:
		return equal(target, filter)
	}
}

func equal(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x == y
	}
	switch a.(type) {
	case string, bool, nil:
		return a == b
	}
	return false
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case uint64:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
