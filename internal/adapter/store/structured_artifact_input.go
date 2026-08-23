package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	"golang.org/x/sys/unix"
)

const maxArtifactRecoveryCandidates = 256

func (r *Repository) ReadArtifactBlobRange(ctx context.Context, expected core.ArtifactBlobRef, offset int64, max int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := expected.Validate(); err != nil || offset < 0 || max < 0 || offset > expected.Size {
		return nil, fmt.Errorf("invalid artifact blob range")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	resolution, err := r.resolveArtifactBlobStateUnlocked(expected)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", structuredapp.ErrArtifactInputUnavailable, err)
	}
	switch resolution.State {
	case ArtifactBlobCompacted:
		return nil, structuredapp.ErrArtifactInputCompacted
	case ArtifactBlobRetained:
		if max == 0 || offset == expected.Size {
			return []byte{}, nil
		}
		remaining := expected.Size - offset
		if int64(max) > remaining {
			max = int(remaining)
		}
		return readValidatedArtifactBlobRange(r.artifactBlobPath(expected.BlobID), expected, offset, max)
	default:
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
}

func (r *Repository) DescribeArtifactInput(ctx context.Context, expected core.ArtifactBlobRef) (structuredapp.InputContext, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.InputContext{}, err
	}
	if err := expected.Validate(); err != nil {
		return structuredapp.InputContext{}, structuredapp.ErrArtifactInputUnavailable
	}

	r.structuredMu.Lock()
	resolution, err := r.resolveArtifactBlobStateUnlocked(expected)
	if err != nil {
		r.structuredMu.Unlock()
		return structuredapp.InputContext{}, fmt.Errorf("%w: %v", structuredapp.ErrArtifactInputUnavailable, err)
	}
	if resolution.State == ArtifactBlobCompacted {
		r.structuredMu.Unlock()
		return structuredapp.InputContext{}, structuredapp.ErrArtifactInputCompacted
	}
	if resolution.State != ArtifactBlobRetained {
		r.structuredMu.Unlock()
		return structuredapp.InputContext{}, structuredapp.ErrArtifactInputUnavailable
	}
	record, err := readCaptureAuthorityRecord(r.captureAuthorityPath(operation.ID(expected.OperationID)))
	r.structuredMu.Unlock()
	if err != nil {
		return structuredapp.InputContext{}, structuredapp.ErrArtifactInputUnavailable
	}
	intent := record.Authority.Intent
	if record.State != structuredapp.CaptureAuthorityPrepared || intent.OperationID != expected.OperationID || intent.SessionID != expected.SessionID ||
		intent.RepositoryID != expected.RepositoryID || intent.WorkspaceID != expected.WorkspaceID || intent.DeclaredPathToken != expected.DeclaredPath ||
		intent.NormalizedWorkspacePath != expected.NormalizedWorkspacePath {
		return structuredapp.InputContext{}, structuredapp.ErrArtifactInputUnavailable
	}
	workspaces, err := r.ListWorkspaces(ctx)
	if err != nil {
		return structuredapp.InputContext{}, err
	}
	for _, workspace := range workspaces {
		if string(workspace.ID) == expected.WorkspaceID && string(workspace.RepositoryID) == expected.RepositoryID {
			return structuredapp.InputContext{OperationID: expected.OperationID, RepositoryRoot: workspace.Root}, nil
		}
	}
	return structuredapp.InputContext{}, structuredapp.ErrArtifactInputUnavailable
}

func readValidatedArtifactBlobRange(blobPath string, expected core.ArtifactBlobRef, offset int64, max int) ([]byte, error) {
	fd, err := unix.Open(blobPath+"/content", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", structuredapp.ErrArtifactInputUnavailable, err)
	}
	file := os.NewFile(uintptr(fd), blobPath+"/content")
	if file == nil {
		_ = unix.Close(fd)
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() != expected.Size {
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, expected.Size+1))
	if err != nil || n != expected.Size || hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
	buf := make([]byte, max)
	readN, err := io.ReadFull(io.NewSectionReader(file, offset, int64(max)), buf)
	if err != nil || readN != max {
		return nil, structuredapp.ErrArtifactInputUnavailable
	}
	return buf, nil
}

func (r *Repository) ListArtifactRecoveryCandidates(ctx context.Context, limit int) ([]structuredapp.ArtifactRecoveryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxArtifactRecoveryCandidates {
		return nil, fmt.Errorf("invalid artifact recovery candidate limit")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	entries, err := boundedPrivateEntries(r.artifactRecoveryRoot())
	if err != nil {
		return nil, err
	}
	candidates := make([]structuredapp.ArtifactRecoveryCandidate, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if len(candidates) == limit {
			break
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unexpected artifact recovery entry")
		}
		captureID := strings.TrimSuffix(entry.Name(), ".json")
		claim, err := readArtifactRecoveryClaim(r.artifactRecoveryPath(captureID))
		if err != nil {
			return nil, err
		}
		ref, err := r.resolveArtifactBlobUnlocked(claim.BlobID)
		if errors.Is(err, ErrNotFound) {
			// Claim-before-blob is a valid pre-commit crash boundary, but there are
			// no immutable bytes to schedule yet.
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := r.validateRecoveryClaimBlobUnlocked(claim); err != nil {
			return nil, err
		}
		record, err := readCaptureAuthorityRecord(r.captureAuthorityPath(operation.ID(claim.OperationID)))
		if err != nil {
			return nil, err
		}
		if !recoveryCandidateMatches(claim, ref, record) {
			return nil, fmt.Errorf("artifact recovery authority mismatch")
		}
		candidates = append(candidates, structuredapp.ArtifactRecoveryCandidate{Ref: ref, CaptureAuthority: record})
	}
	return candidates, nil
}

func recoveryCandidateMatches(claim structuredapp.ArtifactRecoveryClaim, ref core.ArtifactBlobRef, record structuredapp.CaptureAuthorityRecord) bool {
	if claim.Validate() != nil || ref.Validate() != nil || record.Validate() != nil || record.State != structuredapp.CaptureAuthorityPrepared ||
		record.StructuredCaptureDigest != claim.CaptureAuthorityID {
		return false
	}
	intent := record.Authority.Intent
	blobID, err := structuredapp.ArtifactBlobID(intent)
	return err == nil && blobID == claim.BlobID && blobID == ref.BlobID &&
		claim.OperationID == intent.OperationID && claim.SessionID == intent.SessionID && claim.RepositoryID == intent.RepositoryID &&
		claim.WorkspaceID == intent.WorkspaceID && claim.AdapterID == intent.AdapterID && claim.TerminalCut == ref.TerminalCut &&
		ref.OperationID == intent.OperationID && ref.SessionID == intent.SessionID && ref.RepositoryID == intent.RepositoryID &&
		ref.WorkspaceID == intent.WorkspaceID && ref.DeclaredPath == intent.DeclaredPathToken && ref.NormalizedWorkspacePath == intent.NormalizedWorkspacePath
}
