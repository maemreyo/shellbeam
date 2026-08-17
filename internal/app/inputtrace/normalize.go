package inputtrace

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

type NormalizationContext struct {
	WorkspaceRoot string
	ExecutionCWD  string
}

type NormalizationSummary struct {
	Observed  int
	Returned  int
	Truncated bool
}

type normalizationState struct {
	root          string
	cwd           string
	out           []core.Resource
	seen          map[string]struct{}
	externalIDs   map[string]string
	externalCount int
	observed      int
	truncated     bool
}

func NormalizeResources(ctx NormalizationContext, incoming []ProviderResource) ([]core.Resource, NormalizationSummary) {
	ordered := append([]ProviderResource(nil), incoming...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ObservationClass != ordered[j].ObservationClass {
			return ordered[i].ObservationClass < ordered[j].ObservationClass
		}
		return ordered[i].Path < ordered[j].Path
	})
	cwd := canonicalExisting(ctx.ExecutionCWD)
	if cwd == "" && filepath.IsAbs(ctx.ExecutionCWD) {
		cwd = filepath.Clean(ctx.ExecutionCWD)
	}
	state := normalizationState{
		root: canonicalExisting(ctx.WorkspaceRoot), cwd: cwd,
		out:  make([]core.Resource, 0, min(len(ordered), core.MaxPublicResources)),
		seen: map[string]struct{}{}, externalIDs: map[string]string{},
	}
	for _, raw := range ordered {
		state.add(raw)
	}
	sort.Slice(state.out, func(i, j int) bool {
		if state.out[i].ObservationClass != state.out[j].ObservationClass {
			return state.out[i].ObservationClass < state.out[j].ObservationClass
		}
		if state.out[i].PathClass != state.out[j].PathClass {
			return state.out[i].PathClass < state.out[j].PathClass
		}
		return state.out[i].Identity < state.out[j].Identity
	})
	return state.out, NormalizationSummary{Observed: state.observed, Returned: len(state.out), Truncated: state.truncated}
}

func (s *normalizationState) add(raw ProviderResource) {
	resource, ok := s.classify(raw)
	if !ok {
		s.truncated = true
		return
	}
	key := string(resource.ObservationClass) + "\x00" + string(resource.PathClass) + "\x00" + resource.Identity
	if _, duplicate := s.seen[key]; duplicate {
		return
	}
	s.seen[key] = struct{}{}
	s.observed++
	if len(s.out) >= core.MaxPublicResources {
		s.truncated = true
		return
	}
	if err := resource.Validate(); err != nil {
		s.truncated = true
		return
	}
	s.out = append(s.out, resource)
}

func (s *normalizationState) classify(raw ProviderResource) (core.Resource, bool) {
	if !validProviderPath(raw.Path) || !validProviderClass(raw.ObservationClass) {
		return core.Resource{}, false
	}
	absolute := raw.Path
	if !filepath.IsAbs(absolute) {
		if s.cwd == "" {
			absolute = ""
		} else {
			absolute = filepath.Join(s.cwd, raw.Path)
		}
	}
	canonical := canonicalExisting(absolute)
	if canonical != "" && s.root != "" && withinRoot(s.root, canonical) {
		rel, err := filepath.Rel(s.root, canonical)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return core.Resource{}, false
		}
		return core.Resource{ObservationClass: raw.ObservationClass, PathClass: core.PathRepoRelative, Identity: filepath.ToSlash(rel)}, true
	}
	if class := systemClass(canonical); class != "" {
		return core.Resource{ObservationClass: raw.ObservationClass, PathClass: core.PathSystemClassified, Identity: class}, true
	}
	identity, ok := s.externalIdentity(canonical, absolute, raw.Path)
	if !ok {
		return core.Resource{}, false
	}
	return core.Resource{ObservationClass: raw.ObservationClass, PathClass: core.PathWorkspaceExternalRedacted, Identity: identity}, true
}

func (s *normalizationState) externalIdentity(canonical, absolute, raw string) (string, bool) {
	key := canonical
	if key == "" {
		key = filepath.Clean(absolute)
	}
	if key == "" || key == "." {
		key = "unresolved:" + raw
	}
	if identity, ok := s.externalIDs[key]; ok {
		return identity, true
	}
	if s.externalCount >= core.MaxExternalResources {
		return "", false
	}
	s.externalCount++
	identity := "external-" + strconv.Itoa(s.externalCount)
	s.externalIDs[key] = identity
	return identity, true
}

func canonicalExisting(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return ""
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return ""
	}
	return filepath.Clean(absolute)
}
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func systemClass(path string) string {
	if path == "" {
		return ""
	}
	for prefix, kind := range map[string]string{"/usr": "usr", "/System": "system", "/Library": "library", "/bin": "system", "/sbin": "system", "/dev": "device"} {
		if path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator)) {
			return kind
		}
	}
	return ""
}
func validProviderPath(v string) bool {
	if v == "" || len(v) > 4096 || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validProviderClass(v core.ObservationClass) bool {
	switch v {
	case core.ClassFilesystemReads, core.ClassFilesystemMetadataQueries, core.ClassDirectoryEnumerations, core.ClassFilesystemWrites, core.ClassExecutedBinaries, core.ClassLoadedLibraries, core.ClassEnvironmentNamesObserved, core.ClassNetworkAttempts, core.ClassChildProcesses:
		return true
	}
	return false
}
