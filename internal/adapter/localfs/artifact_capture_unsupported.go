//go:build !darwin && !linux

package localfs

import (
	"context"
	"errors"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
)

var (
	ErrArtifactPreexisting     = errors.New("artifact_preexisting_unqualified")
	ErrArtifactPathUnqualified = errors.New("artifact_path_unqualified")
	ErrArtifactAuthorityClosed = errors.New("artifact_path_authority_closed")
)

type ArtifactPathAuthority struct{}

func QualifyArtifactAbsentBaseline(context.Context, string, string) (*ArtifactPathAuthority, structuredapp.CaptureBaselineIdentity, error) {
	return nil, structuredapp.CaptureBaselineIdentity{}, ErrArtifactPathUnqualified
}

func (*ArtifactPathAuthority) NormalizedWorkspacePath() string { return "" }
func (*ArtifactPathAuthority) FinalName() string               { return "" }
func (*ArtifactPathAuthority) BaselineDigest() string          { return "" }
func (*ArtifactPathAuthority) OpenArtifactSource(context.Context, string, int64) (structuredapp.ArtifactSourceHandle, structuredapp.ArtifactSourceIdentity, error) {
	return nil, structuredapp.ArtifactSourceIdentity{}, structuredapp.ErrArtifactCaptureUnavailable
}
func (*ArtifactPathAuthority) Close() error { return nil }

type ArtifactBaselineProvider struct{}

func (ArtifactBaselineProvider) QualifyAbsent(ctx context.Context, workspaceRoot, normalizedWorkspacePath string) (structuredapp.ArtifactPathAuthority, structuredapp.CaptureBaselineIdentity, error) {
	return QualifyArtifactAbsentBaseline(ctx, workspaceRoot, normalizedWorkspacePath)
}
