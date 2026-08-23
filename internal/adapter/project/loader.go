package project

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

type Loader struct{}

func NewLoader() *Loader { return &Loader{} }

func (l *Loader) Load(ctx context.Context, root string) core.LoadResult {
	if err := ctx.Err(); err != nil {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodePathEscape}
	}
	bootstrap := discoverAgentBootstrap(root)
	families, evidence := discoverProjectFamilies(root)
	discoveryFingerprint := ""
	if len(evidence) > 0 {
		discoveryFingerprint = core.RawDigest([]byte(strings.Join(evidence, "\n")))
	}
	manifestPath := filepath.Join(root, ".shellbeam", "project.toml")
	info, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadAbsent, Code: core.CodeManifestAbsent, DiscoveryFingerprint: discoveryFingerprint, DetectedFamilies: families, DiscoveryEvidence: evidence})
	}
	if err != nil {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError})
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, resolveErr := filepath.EvalSymlinks(manifestPath)
		if resolveErr != nil || !withinRoot(root, target) {
			return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodePathEscape})
		}
		manifestPath = target
		info, err = os.Stat(manifestPath)
		if err != nil {
			return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError})
		}
	}
	if !info.Mode().IsRegular() {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError})
	}
	if info.Size() > core.MaxManifestBytes {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeTooLarge})
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError})
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, core.MaxManifestBytes+1))
	if err != nil {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError})
	}
	if len(data) > core.MaxManifestBytes {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, Code: core.CodeTooLarge})
	}
	parsed, err := core.Parse(data)
	if err != nil {
		return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadInvalid, ManifestDigest: core.RawDigest(data), Code: core.ErrorCode(err)})
	}
	digest := core.RawDigest(data)
	return withAgentBootstrap(bootstrap, core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: digest, DiscoveryFingerprint: digest, DetectedFamilies: families, DiscoveryEvidence: evidence})
}
func withAgentBootstrap(bootstrap *core.AgentBootstrap, result core.LoadResult) core.LoadResult {
	result.AgentBootstrap = bootstrap
	return result
}

func discoverAgentBootstrap(root string) *core.AgentBootstrap {
	info, err := os.Lstat(filepath.Join(root, "AGENTS.md"))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return &core.AgentBootstrap{Path: "AGENTS.md", Provenance: core.AgentBootstrapWorkspaceConvention}
}

func discoverProjectFamilies(root string) ([]string, []string) {
	markers := []struct {
		family string
		path   string
	}{
		{"go", "go.mod"},
		{"node", "package.json"},
		{"python", "pyproject.toml"},
		{"python", "requirements.txt"},
		{"rust", "Cargo.toml"},
	}
	familySet := make(map[string]struct{}, len(markers))
	evidence := make([]string, 0, len(markers))
	for _, marker := range markers {
		info, err := os.Lstat(filepath.Join(root, marker.path))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		familySet[marker.family] = struct{}{}
		evidence = append(evidence, marker.family+":"+marker.path)
	}
	families := make([]string, 0, len(familySet))
	for family := range familySet {
		families = append(families, family)
	}
	sort.Strings(families)
	sort.Strings(evidence)
	return families, evidence
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
