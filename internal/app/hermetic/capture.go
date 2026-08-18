package hermetic

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceContext struct {
	WorkspaceID      workspacecore.WorkspaceID
	RepositoryID     workspacecore.RepositoryID
	Root             string
	SourceGeneration string
}

type WorkspaceSource interface {
	ResolveFresh(context.Context, string) (WorkspaceContext, error)
}

type ProviderCaptureRequest struct {
	WorkspaceID      workspacecore.WorkspaceID
	RepositoryID     workspacecore.RepositoryID
	Root             string
	SourceGeneration string
	Selectors        []string
	Limits           core.CaptureLimits
}

type CapturedView struct {
	CaptureID   string
	PrivateRoot string
	Manifest    core.CaptureManifest
}

type CaptureProvider interface {
	Capture(context.Context, ProviderCaptureRequest) (CapturedView, error)
	Discard(context.Context, CapturedView) error
}

type CaptureService struct {
	workspace WorkspaceSource
	provider  CaptureProvider
	limits    core.CaptureLimits
}

func NewCaptureService(workspace WorkspaceSource, provider CaptureProvider, limits core.CaptureLimits) *CaptureService {
	return &CaptureService{workspace: workspace, provider: provider, limits: limits}
}

func (s *CaptureService) Capture(ctx context.Context, workspaceID string, request core.Request) (CapturedView, error) {
	if err := ctx.Err(); err != nil {
		return CapturedView{}, err
	}
	if s == nil || s.workspace == nil || s.provider == nil {
		return CapturedView{}, fmt.Errorf("hermetic capture unavailable")
	}
	if err := s.limits.Validate(); err != nil {
		return CapturedView{}, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return CapturedView{}, err
	}
	before, err := s.workspace.ResolveFresh(ctx, workspaceID)
	if err != nil {
		return CapturedView{}, err
	}
	if err := validateWorkspaceContext(workspaceID, before); err != nil {
		return CapturedView{}, err
	}
	view, err := s.provider.Capture(ctx, ProviderCaptureRequest{
		WorkspaceID: before.WorkspaceID, RepositoryID: before.RepositoryID, Root: before.Root,
		SourceGeneration: before.SourceGeneration, Selectors: append([]string(nil), canonical.RepoInputs...), Limits: s.limits,
	})
	if err != nil {
		return CapturedView{}, err
	}
	if err := s.validateCapturedView(view, before, canonical); err != nil {
		_ = s.provider.Discard(context.Background(), view)
		return CapturedView{}, err
	}
	after, err := s.workspace.ResolveFresh(ctx, workspaceID)
	if err != nil {
		_ = s.provider.Discard(context.Background(), view)
		return CapturedView{}, err
	}
	if err := validateSameSourceCut(before, after); err != nil {
		_ = s.provider.Discard(context.Background(), view)
		return CapturedView{}, err
	}
	return view, nil
}

func (s *CaptureService) validateCapturedView(view CapturedView, source WorkspaceContext, request core.Request) error {
	if !validCaptureID(view.CaptureID) || !filepath.IsAbs(view.PrivateRoot) || filepath.Clean(view.PrivateRoot) != view.PrivateRoot {
		return fmt.Errorf("invalid hermetic private capture view")
	}
	if err := view.Manifest.Validate(); err != nil {
		return err
	}
	if view.Manifest.WorkspaceID != source.WorkspaceID || view.Manifest.SourceGeneration != source.SourceGeneration || !reflect.DeepEqual(view.Manifest.Selectors, request.RepoInputs) {
		return fmt.Errorf("hermetic provider capture binding mismatch")
	}
	if len(view.Manifest.Entries) > s.limits.MaxPaths || view.Manifest.TotalBytes > s.limits.MaxTotalBytes {
		return fmt.Errorf("hermetic provider capture exceeds request limits")
	}
	for _, entry := range view.Manifest.Entries {
		if entry.Size > s.limits.MaxFileBytes {
			return fmt.Errorf("hermetic provider file exceeds request limit")
		}
	}
	return nil
}

func validateWorkspaceContext(requested string, got WorkspaceContext) error {
	parsed, err := workspacecore.ParseWorkspaceID(requested)
	if err != nil || got.WorkspaceID != parsed {
		return fmt.Errorf("hermetic workspace binding mismatch")
	}
	if _, err := workspacecore.ParseRepositoryID(string(got.RepositoryID)); err != nil {
		return fmt.Errorf("hermetic repository binding invalid")
	}
	if !filepath.IsAbs(got.Root) || filepath.Clean(got.Root) != got.Root {
		return fmt.Errorf("hermetic workspace root invalid")
	}
	if err := core.ValidateSourceGeneration(got.SourceGeneration); err != nil {
		return err
	}
	return nil
}

func validateSameSourceCut(before, after WorkspaceContext) error {
	if err := validateWorkspaceContext(string(before.WorkspaceID), after); err != nil {
		return err
	}
	if before.RepositoryID != after.RepositoryID || before.Root != after.Root || before.SourceGeneration != after.SourceGeneration {
		return fmt.Errorf("hermetic source generation changed during capture")
	}
	return nil
}

func validCaptureID(value string) bool {
	if !strings.HasPrefix(value, "hcap_") || len(value) < 10 || len(value) > 64 || strings.ContainsAny(value, "/\\") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "hcap_") {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
