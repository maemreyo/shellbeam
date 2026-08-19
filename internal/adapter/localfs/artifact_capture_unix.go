//go:build darwin || linux

package localfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"golang.org/x/sys/unix"
)

var (
	ErrArtifactPreexisting     = errors.New("artifact_preexisting_unqualified")
	ErrArtifactPathUnqualified = errors.New("artifact_path_unqualified")
	ErrArtifactAuthorityClosed = errors.New("artifact_path_authority_closed")
)

const (
	artifactPathAuthoritySchemaV1 = 1
	maxArtifactPathBytes          = 4096
	maxArtifactPathComponents     = 128
)

type artifactCaptureHooks struct {
	checkpoint func(stage string)
}

type artifactBaselineResolver struct {
	hooks *artifactCaptureHooks
}

type artifactDirIdentity struct {
	Dev uint64 `json:"dev"`
	Ino uint64 `json:"ino"`
}

type ArtifactPathAuthority struct {
	mu                      sync.Mutex
	parentFD                int
	finalName               string
	normalizedWorkspacePath string
	baselineDigest          string
	rootIdentity            artifactDirIdentity
	parentIdentity          artifactDirIdentity
	hooks                   *artifactCaptureHooks
	closed                  bool
}

func QualifyArtifactAbsentBaseline(ctx context.Context, workspaceRoot, normalizedWorkspacePath string) (*ArtifactPathAuthority, structuredapp.CaptureBaselineIdentity, error) {
	return (artifactBaselineResolver{}).qualifyAbsent(ctx, workspaceRoot, normalizedWorkspacePath)
}

func (r artifactBaselineResolver) qualifyAbsent(ctx context.Context, workspaceRoot, normalizedWorkspacePath string) (*ArtifactPathAuthority, structuredapp.CaptureBaselineIdentity, error) {
	components, err := validateArtifactAuthorityPath(workspaceRoot, normalizedWorkspacePath)
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	if err := r.checkpoint(ctx, "before-root-open"); err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	rootFD, err := unix.Open(workspaceRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, artifactPathError(err)
	}
	rootOwned := true
	defer func() {
		if rootOwned {
			_ = unix.Close(rootFD)
		}
	}()
	if err := r.checkpoint(ctx, "after-root-open"); err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	rootIdentity, err := statArtifactDir(rootFD)
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}

	parentFD, err := r.traverseParent(ctx, rootFD, components[:len(components)-1])
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	parentOwned := parentFD != rootFD
	defer func() {
		if parentOwned {
			_ = unix.Close(parentFD)
		}
	}()
	parentIdentity, err := statArtifactDir(parentFD)
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	finalName := components[len(components)-1]
	if err := requireArtifactAbsent(parentFD, finalName); err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	if err := r.checkpoint(ctx, "after-absent-check"); err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}

	if err := r.verifyAbsentBinding(ctx, workspaceRoot, rootFD, components, rootIdentity, parentIdentity, finalName); err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}

	digest, err := artifactBaselineDigest(rootIdentity, parentIdentity, normalizedWorkspacePath, finalName)
	if err != nil {
		return nil, structuredapp.CaptureBaselineIdentity{}, err
	}
	authority := &ArtifactPathAuthority{
		parentFD: parentFD, finalName: finalName, normalizedWorkspacePath: normalizedWorkspacePath,
		baselineDigest: digest, rootIdentity: rootIdentity, parentIdentity: parentIdentity, hooks: r.hooks,
	}
	if parentFD == rootFD {
		rootOwned = false
	} else {
		parentOwned = false
	}
	baseline := structuredapp.CaptureBaselineIdentity{
		SchemaVersion:   structuredapp.CaptureBaselineSchemaV1,
		State:           structuredapp.CaptureBaselineAbsent,
		AuthorityDigest: digest,
	}
	return authority, baseline, nil
}

func (r artifactBaselineResolver) verifyAbsentBinding(ctx context.Context, workspaceRoot string, rootFD int, components []string, rootIdentity, parentIdentity artifactDirIdentity, finalName string) error {
	if err := verifyWorkspaceRootIdentity(workspaceRoot, rootIdentity); err != nil {
		return err
	}
	verifyParentFD, err := r.traverseParent(ctx, rootFD, components[:len(components)-1])
	if err != nil {
		return err
	}
	if verifyParentFD != rootFD {
		defer unix.Close(verifyParentFD)
	}
	verifyIdentity, err := statArtifactDir(verifyParentFD)
	if err != nil || verifyIdentity != parentIdentity {
		return ErrArtifactPathUnqualified
	}
	if err := requireArtifactAbsent(verifyParentFD, finalName); err != nil {
		if errors.Is(err, ErrArtifactPreexisting) {
			return ErrArtifactPathUnqualified
		}
		return err
	}
	return nil
}

func (r artifactBaselineResolver) checkpoint(ctx context.Context, stage string) error {
	if r.hooks != nil && r.hooks.checkpoint != nil {
		r.hooks.checkpoint(stage)
	}
	return ctx.Err()
}

func (r artifactBaselineResolver) traverseParent(ctx context.Context, rootFD int, components []string) (int, error) {
	current := rootFD
	owned := false
	for _, component := range components {
		if err := r.checkpoint(ctx, "before-openat-parent"); err != nil {
			if owned {
				_ = unix.Close(current)
			}
			return -1, err
		}
		next, err := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			if owned {
				_ = unix.Close(current)
			}
			return -1, artifactPathError(err)
		}
		if err := r.checkpoint(ctx, "after-openat-parent"); err != nil {
			_ = unix.Close(next)
			if owned {
				_ = unix.Close(current)
			}
			return -1, err
		}
		if owned {
			_ = unix.Close(current)
		}
		current = next
		owned = true
	}
	return current, nil
}

func requireArtifactAbsent(parentFD int, finalName string) error {
	var st unix.Stat_t
	err := unix.Fstatat(parentFD, finalName, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err == nil {
		return ErrArtifactPreexisting
	}
	return artifactPathError(err)
}

func verifyWorkspaceRootIdentity(workspaceRoot string, want artifactDirIdentity) error {
	fd, err := unix.Open(workspaceRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return artifactPathError(err)
	}
	defer unix.Close(fd)
	got, err := statArtifactDir(fd)
	if err != nil || got != want {
		return ErrArtifactPathUnqualified
	}
	return nil
}

func statArtifactDir(fd int) (artifactDirIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return artifactDirIdentity{}, artifactPathError(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return artifactDirIdentity{}, ErrArtifactPathUnqualified
	}
	return artifactDirIdentity{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, nil
}

func artifactBaselineDigest(rootIdentity, parentIdentity artifactDirIdentity, normalizedPath, finalName string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version                 int                 `json:"version"`
		State                   string              `json:"state"`
		RootIdentity            artifactDirIdentity `json:"root_identity"`
		ParentIdentity          artifactDirIdentity `json:"parent_identity"`
		NormalizedWorkspacePath string              `json:"normalized_workspace_path"`
		FinalName               string              `json:"final_name"`
	}{
		Version: artifactPathAuthoritySchemaV1, State: structuredapp.CaptureBaselineAbsent,
		RootIdentity: rootIdentity, ParentIdentity: parentIdentity,
		NormalizedWorkspacePath: normalizedPath, FinalName: finalName,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validateArtifactAuthorityPath(workspaceRoot, normalized string) ([]string, error) {
	if !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot || !safeArtifactPathText(normalized) || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, `\`) || path.Clean(normalized) != normalized {
		return nil, ErrArtifactPathUnqualified
	}
	components := strings.Split(normalized, "/")
	if len(components) == 0 || len(components) > maxArtifactPathComponents {
		return nil, ErrArtifactPathUnqualified
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, ErrArtifactPathUnqualified
		}
	}
	return components, nil
}

func safeArtifactPathText(value string) bool {
	if value == "" || len(value) > maxArtifactPathBytes || strings.ContainsRune(value, 0) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func artifactPathError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrArtifactPathUnqualified, err)
}

func (a *ArtifactPathAuthority) NormalizedWorkspacePath() string {
	if a == nil {
		return ""
	}
	return a.normalizedWorkspacePath
}

func (a *ArtifactPathAuthority) FinalName() string {
	if a == nil {
		return ""
	}
	return a.finalName
}

func (a *ArtifactPathAuthority) BaselineDigest() string {
	if a == nil {
		return ""
	}
	return a.baselineDigest
}

type ArtifactBaselineProvider struct{}

func (ArtifactBaselineProvider) QualifyAbsent(ctx context.Context, workspaceRoot, normalizedWorkspacePath string) (structuredapp.ArtifactPathAuthority, structuredapp.CaptureBaselineIdentity, error) {
	return QualifyArtifactAbsentBaseline(ctx, workspaceRoot, normalizedWorkspacePath)
}
