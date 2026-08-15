package mutationscope

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func NormalizeSelectors(in []string) ([]string, error) {
	if len(in) == 0 || len(in) > MaxPathsPerScope {
		return nil, fmt.Errorf("invalid selector count")
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		if err := validateSelector(raw); err != nil {
			return nil, err
		}
		if _, exists := seen[raw]; exists {
			return nil, fmt.Errorf("duplicate selector")
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	sort.Strings(out)
	return out, nil
}

func validateSelector(v string) error {
	if v == "**" {
		return nil
	}
	if v == "" || len(v) > MaxSelectorBytes || !utf8.ValidString(v) || strings.HasPrefix(v, "/") || strings.Contains(v, `\`) {
		return fmt.Errorf("invalid selector")
	}
	base := v
	if strings.HasSuffix(v, "/**") {
		base = strings.TrimSuffix(v, "/**")
		if base == "" {
			return fmt.Errorf("invalid selector")
		}
	} else if strings.ContainsAny(v, "*?[]{}") {
		return fmt.Errorf("invalid selector")
	}
	if strings.Contains(base, "**") || strings.ContainsAny(base, "*?[]{}") {
		return fmt.Errorf("invalid selector")
	}
	parts := strings.Split(base, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid selector")
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return fmt.Errorf("invalid selector")
			}
		}
	}
	return nil
}

func SelectorsOverlap(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if selectorOverlap(left, right) {
				return true
			}
		}
	}
	return false
}

func selectorOverlap(a, b string) bool {
	if a == "**" || b == "**" {
		return true
	}
	abase, asub := subtreeBase(a)
	bbase, bsub := subtreeBase(b)
	switch {
	case !asub && !bsub:
		return abase == bbase
	case asub && !bsub:
		return bbase == abase || strings.HasPrefix(bbase, abase+"/")
	case !asub && bsub:
		return abase == bbase || strings.HasPrefix(abase, bbase+"/")
	default:
		return abase == bbase || strings.HasPrefix(abase, bbase+"/") || strings.HasPrefix(bbase, abase+"/")
	}
}

func subtreeBase(v string) (string, bool) {
	if strings.HasSuffix(v, "/**") {
		return strings.TrimSuffix(v, "/**"), true
	}
	return v, false
}
