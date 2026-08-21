package main

import (
	"bytes"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type SelectionReason struct {
	Suite   string   `json:"suite"`
	Paths   []string `json:"paths"`
	Mapping string   `json:"mapping"`
}

type impactConfig struct {
	Version  int             `toml:"version"`
	Mappings []impactMapping `toml:"mapping"`
	Global   []string        `toml:"global"`
}

type impactMapping struct {
	Glob   string   `toml:"glob"`
	Suites []string `toml:"suites"`
}

type impactSelection struct {
	Mode    string
	Suites  []string
	Reasons []SelectionReason
}

type reasonKey struct {
	suite   string
	mapping string
}

func loadImpactConfig(filename string) (impactConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return impactConfig{}, err
	}
	return parseImpactConfig(data)
}

func parseImpactConfig(data []byte) (impactConfig, error) {
	var cfg impactConfig
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg); err != nil {
		return impactConfig{}, fmt.Errorf("test impact config: %w", err)
	}
	if cfg.Version != 1 {
		return impactConfig{}, fmt.Errorf("test impact config: unsupported version %d", cfg.Version)
	}
	for i, mapping := range cfg.Mappings {
		if mapping.Glob == "" || len(mapping.Suites) == 0 {
			return impactConfig{}, fmt.Errorf("test impact config: mapping %d requires glob and suites", i)
		}
	}
	return cfg, nil
}

func selectImpact(cfg impactConfig, changed []string) impactSelection {
	paths := normalizedPaths(changed)
	if len(paths) == 0 {
		return impactSelection{Mode: "empty"}
	}
	if reasons := matchingGlobalReasons(cfg.Global, paths); len(reasons) != 0 {
		return impactSelection{Mode: "global", Suites: []string{"./..."}, Reasons: reasons}
	}

	reasonPaths := map[reasonKey]map[string]bool{}
	suites := map[string]bool{}
	mapped := map[string]bool{}
	for _, mapping := range cfg.Mappings {
		for _, changedPath := range paths {
			if !globMatch(mapping.Glob, changedPath) {
				continue
			}
			mapped[changedPath] = true
			for _, suite := range mapping.Suites {
				suites[suite] = true
				addReasonPath(reasonPaths, reasonKey{suite, mapping.Glob}, changedPath)
			}
		}
	}
	for _, changedPath := range paths {
		if mapped[changedPath] || !strings.HasSuffix(changedPath, ".go") {
			continue
		}
		dir := pathpkg.Dir(changedPath)
		if dir == "." || dir == "" {
			continue
		}
		suite := "./" + dir
		suites[suite] = true
		addReasonPath(reasonPaths, reasonKey{suite, "package"}, changedPath)
	}
	return impactSelection{Mode: selectionMode(suites), Suites: sortedSet(suites), Reasons: sortedReasons(reasonPaths)}
}

func matchingGlobalReasons(patterns, paths []string) []SelectionReason {
	var reasons []SelectionReason
	for _, pattern := range patterns {
		var matched []string
		for _, changedPath := range paths {
			if globMatch(pattern, changedPath) {
				matched = append(matched, changedPath)
			}
		}
		if len(matched) != 0 {
			reasons = append(reasons, SelectionReason{Suite: "./...", Paths: matched, Mapping: pattern})
		}
	}
	sort.Slice(reasons, func(i, j int) bool { return reasons[i].Mapping < reasons[j].Mapping })
	return reasons
}

func addReasonPath(reasons map[reasonKey]map[string]bool, key reasonKey, changedPath string) {
	if reasons[key] == nil {
		reasons[key] = map[string]bool{}
	}
	reasons[key][changedPath] = true
}

func sortedReasons(reasons map[reasonKey]map[string]bool) []SelectionReason {
	out := make([]SelectionReason, 0, len(reasons))
	for key, paths := range reasons {
		out = append(out, SelectionReason{Suite: key.suite, Mapping: key.mapping, Paths: sortedSet(paths)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Suite == out[j].Suite {
			return out[i].Mapping < out[j].Mapping
		}
		return out[i].Suite < out[j].Suite
	})
	return out
}

func selectionMode(suites map[string]bool) string {
	if len(suites) == 0 {
		return "empty"
	}
	return "affected"
}

func normalizedPaths(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		p = strings.TrimPrefix(filepath.ToSlash(p), "./")
		if p != "" {
			set[p] = true
		}
	}
	return sortedSet(set)
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func globMatch(pattern, value string) bool {
	return matchGlobParts(strings.Split(pathpkg.Clean(pattern), "/"), strings.Split(pathpkg.Clean(value), "/"))
}

func matchGlobParts(pattern, value []string) bool {
	if len(pattern) == 0 {
		return len(value) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(value); i++ {
			if matchGlobParts(pattern[1:], value[i:]) {
				return true
			}
		}
		return false
	}
	if len(value) == 0 {
		return false
	}
	matched, err := pathpkg.Match(pattern[0], value[0])
	return err == nil && matched && matchGlobParts(pattern[1:], value[1:])
}

func testSelection(args []string, changed []string) (impactSelection, error) {
	if !hasArg(args, "--dirty") || hasArg(args, "--full") {
		packages, err := listPackages()
		if err != nil {
			return impactSelection{}, err
		}
		return impactSelection{Mode: "full", Suites: packages}, nil
	}
	cfg, err := loadImpactConfig("dev/test-impact.toml")
	if err != nil {
		return impactSelection{}, err
	}
	selection := selectImpact(cfg, changed)
	if deletedFallbackPackage(selection) {
		return impactSelection{Mode: "global", Suites: []string{"./..."}, Reasons: selection.Reasons}, nil
	}
	return selection, nil
}

func deletedFallbackPackage(selection impactSelection) bool {
	for _, reason := range selection.Reasons {
		if reason.Mapping != "package" || !strings.HasPrefix(reason.Suite, "./") {
			continue
		}
		dir := strings.TrimPrefix(reason.Suite, "./")
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return true
			}
			continue
		}
		hasGo := false
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
				hasGo = true
				break
			}
		}
		if !hasGo {
			return true
		}
	}
	return false
}

func goSuites(suites []string) ([]string, error) {
	set := map[string]bool{}
	for _, suite := range suites {
		switch {
		case suite == "contract:markdown":
			set["./tests/contract"] = true
		case strings.HasPrefix(suite, "./") || !strings.Contains(suite, ":"):
			set[suite] = true
		default:
			return nil, fmt.Errorf("unknown test suite %q", suite)
		}
	}
	return sortedSet(set), nil
}
