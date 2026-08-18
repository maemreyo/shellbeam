//go:build linux || darwin

package localfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

// ScopeMeasurer re-observes only the repo inputs already authorized by a
// ProvenInputScope. It materializes no private capture and has no execution
// authority; its only output is a content-only digest for evidence validity.
type ScopeMeasurer struct {
	limits core.CaptureLimits
}

func NewScopeMeasurer(limits core.CaptureLimits) (*ScopeMeasurer, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &ScopeMeasurer{limits: limits}, nil
}

func (m *ScopeMeasurer) ObserveProvenScope(ctx context.Context, req evidenceapp.ProvenScopeObservationRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if m == nil {
		return "", fmt.Errorf("hermetic scope observer unavailable")
	}
	if err := m.limits.Validate(); err != nil {
		return "", err
	}
	workspaceID, err := workspacecore.ParseWorkspaceID(req.WorkspaceID)
	if err != nil {
		return "", err
	}
	if err := core.ValidateSourceGeneration(req.SourceGeneration); err != nil {
		return "", err
	}
	if err := req.Scope.Validate(); err != nil {
		return "", err
	}
	if err := validateScopeRoot(req.WorkspaceRoot); err != nil {
		return "", err
	}
	selectors, err := canonicalSelectors(req.Scope.RepoInputs)
	if err != nil || !sameSelectorList(selectors, req.Scope.RepoInputs) {
		return "", fmt.Errorf("noncanonical proven input scope")
	}
	selected, err := selectSourceFiles(ctx, req.WorkspaceRoot, selectors, m.limits)
	if err != nil {
		return "", err
	}
	baseline, err := baselineSourceFiles(ctx, req.WorkspaceRoot, selected, m.limits)
	if err != nil {
		return "", err
	}
	entries := make([]core.CaptureEntry, 0, len(baseline))
	total := int64(0)
	for _, source := range baseline {
		total += source.Size
		entries = append(entries, core.CaptureEntry{Path: source.Path, Size: source.Size, SHA256: source.SHA256, Executable: source.Executable})
	}
	manifest := core.CaptureManifest{SchemaVersion: core.CaptureManifestSchemaVersion, WorkspaceID: workspaceID, SourceGeneration: req.SourceGeneration, Selectors: selectors, Entries: entries, TotalBytes: total}
	manifest, err = manifest.Canonical()
	if err != nil {
		return "", err
	}
	if err := verifySourceUnchanged(ctx, req.WorkspaceRoot, selectors, m.limits, selected, manifest); err != nil {
		return "", err
	}
	return manifest.ContentDigest()
}

func validateScopeRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return fmt.Errorf("invalid hermetic scope root")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic scope root")
	}
	return nil
}

func sameSelectorList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ evidenceapp.ProvenScopeObserver = (*ScopeMeasurer)(nil)
