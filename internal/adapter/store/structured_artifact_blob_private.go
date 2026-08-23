package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	"golang.org/x/sys/unix"
)

const (
	artifactBlobMetadataSchemaV1 = 1
	artifactBlobCommitRetained   = "retained"
	maxArtifactBlobMetadataBytes = int64(64 << 10)
)

type artifactBlobMetadata struct {
	SchemaVersion       int                                           `json:"schema_version"`
	CommitState         string                                        `json:"commit_state"`
	CaptureIntentDigest string                                        `json:"capture_intent_digest"`
	Intent              structuredapp.ArtifactCaptureIntent           `json:"capture_intent"`
	Ref                 core.ArtifactBlobRef                          `json:"artifact_blob_ref"`
	Observation         structuredapp.ArtifactCaptureObservationCutV1 `json:"observation_cut_payload"`
}

func (r *Repository) artifactBlobRoot() string {
	return filepath.Join(r.structuredRoot(), "artifact-blobs")
}
func (r *Repository) artifactRecoveryRoot() string {
	return filepath.Join(r.structuredRoot(), "artifact-recovery")
}
func (r *Repository) artifactBlobPath(blobID string) string {
	return filepath.Join(r.artifactBlobRoot(), blobID)
}
func (r *Repository) artifactRecoveryPath(captureID string) string {
	return filepath.Join(r.artifactRecoveryRoot(), captureID+".json")
}

func validateArtifactBlobMetadata(metadata artifactBlobMetadata) error {
	if metadata.SchemaVersion != artifactBlobMetadataSchemaV1 || metadata.CommitState != artifactBlobCommitRetained || metadata.Intent.Validate() != nil || metadata.Ref.Validate() != nil || metadata.Observation.Validate() != nil {
		return fmt.Errorf("invalid artifact blob metadata")
	}
	intentDigest, err := metadata.Intent.Digest()
	if err != nil || intentDigest != metadata.CaptureIntentDigest || metadata.Observation.CaptureIntentDigest != intentDigest || metadata.Observation.BaselineAuthorityDigest != metadata.Intent.Baseline.AuthorityDigest {
		return fmt.Errorf("artifact blob capture authority mismatch")
	}
	blobID, err := structuredapp.ArtifactBlobID(metadata.Intent)
	if err != nil || blobID != metadata.Ref.BlobID {
		return fmt.Errorf("artifact blob identity mismatch")
	}
	observationDigest, err := metadata.Observation.Digest()
	if err != nil || observationDigest != metadata.Ref.ObservationCut.Digest {
		return fmt.Errorf("artifact blob observation cut mismatch")
	}
	if metadata.Ref.OperationID != metadata.Intent.OperationID || metadata.Ref.SessionID != metadata.Intent.SessionID ||
		metadata.Ref.RepositoryID != metadata.Intent.RepositoryID || metadata.Ref.WorkspaceID != metadata.Intent.WorkspaceID ||
		metadata.Ref.DeclaredPath != metadata.Intent.DeclaredPathToken || metadata.Ref.NormalizedWorkspacePath != metadata.Intent.NormalizedWorkspacePath ||
		metadata.Ref.Size != metadata.Observation.FinalSize {
		return fmt.Errorf("artifact blob provenance mismatch")
	}
	return nil
}

func readArtifactBlobMetadata(path string) (artifactBlobMetadata, error) {
	var metadata artifactBlobMetadata
	if err := readPrivateJSON(filepath.Join(path, "metadata.json"), maxArtifactBlobMetadataBytes, &metadata); err != nil {
		return metadata, err
	}
	return metadata, validateArtifactBlobMetadata(metadata)
}

func readArtifactBlobContent(path string, want core.ArtifactBlobRef) error {
	fd, err := unix.Open(filepath.Join(path, "content"), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(path, "content"))
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open artifact blob content")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() != want.Size || info.Size() < 0 || info.Size() > structuredapp.MaxArtifactBlobBytes {
		return fmt.Errorf("unsafe artifact blob content")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, want.Size+1))
	if err != nil || n != want.Size || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		return fmt.Errorf("artifact blob content mismatch")
	}
	return nil
}

func (r *Repository) resolveArtifactBlobUnlocked(blobID string) (core.ArtifactBlobRef, error) {
	if !validArtifactBlobID(blobID) {
		return core.ArtifactBlobRef{}, fmt.Errorf("invalid artifact blob id")
	}
	path := r.artifactBlobPath(blobID)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return core.ArtifactBlobRef{}, ErrNotFound
	}
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) {
		return core.ArtifactBlobRef{}, fmt.Errorf("unsafe artifact blob directory")
	}
	metadata, err := readArtifactBlobMetadata(path)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if metadata.Ref.BlobID != blobID {
		return core.ArtifactBlobRef{}, fmt.Errorf("artifact blob destination mismatch")
	}
	if err := readArtifactBlobContent(path, metadata.Ref); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	return metadata.Ref, nil
}

func sameArtifactBlobCommit(metadata artifactBlobMetadata, request structuredapp.ArtifactBlobCommitRequest) bool {
	if validateArtifactBlobMetadata(metadata) != nil || request.Validate() != nil {
		return false
	}
	return metadata.CaptureIntentDigest == request.CaptureAuthorityID && reflect.DeepEqual(metadata.Intent, request.Intent) &&
		metadata.Ref.BlobID == request.BlobID && metadata.Ref.TerminalCut == request.TerminalCut &&
		metadata.Observation.PhaseASourceIdentityDigest == request.PreSourceIdentity.Digest && metadata.Observation.PhaseASize == request.PreSourceIdentity.Size
}

type stagedArtifactBlob struct {
	path          string
	ref           core.ArtifactBlobRef
	metadataBytes int64
	contentBytes  int64
}

func (r *Repository) stageArtifactBlob(request structuredapp.ArtifactBlobCommitRequest) (stage stagedArtifactBlob, err error) {
	pre, err := request.Source.StatIdentity()
	if err != nil {
		return stage, err
	}
	if pre != request.PreSourceIdentity {
		return stage, structuredapp.ErrArtifactChangedDuringCapture
	}
	path, err := os.MkdirTemp(r.artifactBlobRoot(), ".artifact-stage-")
	if err != nil {
		return stage, err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		_ = os.RemoveAll(path)
		return stage, err
	}
	stage.path = path
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(path)
		}
	}()
	contentDigest, written, post, err := r.streamArtifactBlobContent(path, request)
	if err != nil {
		return stage, err
	}
	ref, metadata, err := buildArtifactBlobMetadata(request, contentDigest, written, post)
	if err != nil {
		return stage, err
	}
	metadataBytes, err := r.writeArtifactBlobMetadata(path, metadata)
	if err != nil {
		return stage, err
	}
	if err := r.syncArtifactBlobStage(path); err != nil {
		return stage, err
	}
	stage.ref, stage.metadataBytes, stage.contentBytes = ref, metadataBytes, written
	complete = true
	return stage, nil
}

func (r *Repository) streamArtifactBlobContent(stage string, request structuredapp.ArtifactBlobCommitRequest) (string, int64, structuredapp.ArtifactSourceIdentity, error) {
	content, err := os.OpenFile(filepath.Join(stage, "content"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, structuredapp.ArtifactSourceIdentity{}, err
	}
	open := true
	defer func() {
		if open {
			_ = content.Close()
		}
	}()
	if err := r.writer.checkpoint("artifact_blob.content_write"); err != nil {
		return "", 0, structuredapp.ArtifactSourceIdentity{}, err
	}
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(content, h), io.LimitReader(request.Source, request.PreSourceIdentity.Size+1))
	if err != nil {
		return "", 0, structuredapp.ArtifactSourceIdentity{}, err
	}
	post, err := request.Source.StatIdentity()
	if err != nil {
		return "", 0, structuredapp.ArtifactSourceIdentity{}, err
	}
	if written != request.PreSourceIdentity.Size || post != request.PreSourceIdentity {
		return "", 0, post, structuredapp.ErrArtifactChangedDuringCapture
	}
	if err := r.writer.checkpoint("artifact_blob.content_sync"); err != nil {
		return "", 0, post, err
	}
	if err := content.Sync(); err != nil {
		return "", 0, post, err
	}
	if err := content.Close(); err != nil {
		return "", 0, post, err
	}
	open = false
	return hex.EncodeToString(h.Sum(nil)), written, post, nil
}

func buildArtifactBlobMetadata(request structuredapp.ArtifactBlobCommitRequest, contentDigest string, written int64, post structuredapp.ArtifactSourceIdentity) (core.ArtifactBlobRef, artifactBlobMetadata, error) {
	observation := structuredapp.ArtifactCaptureObservationCutV1{
		SchemaVersion:       structuredapp.ArtifactCaptureObservationCutSchemaV1,
		CaptureIntentDigest: request.CaptureAuthorityID, BaselineAuthorityDigest: request.Intent.Baseline.AuthorityDigest,
		SourceObservationScheme:    request.PreSourceIdentity.Scheme,
		PhaseASourceIdentityDigest: request.PreSourceIdentity.Digest, PhaseASize: request.PreSourceIdentity.Size,
		FinalSourceIdentityDigest: post.Digest, FinalSize: post.Size, StabilityResult: structuredapp.ArtifactSourceStabilityStable,
	}
	observationDigest, err := observation.Digest()
	if err != nil {
		return core.ArtifactBlobRef{}, artifactBlobMetadata{}, err
	}
	ref := core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: request.BlobID,
		OperationID: request.Intent.OperationID, SessionID: request.Intent.SessionID,
		RepositoryID: request.Intent.RepositoryID, WorkspaceID: request.Intent.WorkspaceID,
		DeclaredPath: request.Intent.DeclaredPathToken, NormalizedWorkspacePath: request.Intent.NormalizedWorkspacePath,
		SHA256: contentDigest, Size: written, TerminalCut: request.TerminalCut,
		ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: observationDigest},
	}
	metadata := artifactBlobMetadata{
		SchemaVersion: artifactBlobMetadataSchemaV1, CommitState: artifactBlobCommitRetained,
		CaptureIntentDigest: request.CaptureAuthorityID, Intent: request.Intent, Ref: ref, Observation: observation,
	}
	if err := validateArtifactBlobMetadata(metadata); err != nil {
		return core.ArtifactBlobRef{}, artifactBlobMetadata{}, err
	}
	return ref, metadata, nil
}

func (r *Repository) writeArtifactBlobMetadata(stage string, metadata artifactBlobMetadata) (int64, error) {
	encoded, err := json.Marshal(metadata)
	if err != nil || int64(len(encoded)+1) > maxArtifactBlobMetadataBytes {
		if err == nil {
			err = fmt.Errorf("artifact blob metadata exceeds bound")
		}
		return 0, err
	}
	file, err := os.OpenFile(filepath.Join(stage, "metadata.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := r.writer.checkpoint("artifact_blob.metadata_write"); err != nil {
		return 0, err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return 0, err
	}
	if err := r.writer.checkpoint("artifact_blob.metadata_sync"); err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	return int64(len(encoded) + 1), nil
}

func (r *Repository) syncArtifactBlobStage(stage string) error {
	dir, err := os.Open(stage)
	if err != nil {
		return err
	}
	syncErr := r.writer.checkpoint("artifact_blob.stage_sync")
	if syncErr == nil {
		syncErr = dir.Sync()
	}
	closeErr := dir.Close()
	if syncErr == nil {
		syncErr = closeErr
	}
	return syncErr
}

func (r *Repository) publishArtifactBlobStage(request structuredapp.ArtifactBlobCommitRequest, stage stagedArtifactBlob) (core.ArtifactBlobRef, error) {
	if existing, found, err := r.resolveExistingArtifactCommit(request); found || err != nil {
		if err == nil {
			r.consumeBlobReservation(request.Reservation)
		}
		return existing, err
	}
	finalPath := r.artifactBlobPath(request.BlobID)
	if err := r.writer.checkpoint("artifact_blob.rename"); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if err := os.Rename(stage.path, finalPath); err != nil {
		if existing, found, resolveErr := r.resolveExistingArtifactCommit(request); found && resolveErr == nil {
			r.consumeBlobReservation(request.Reservation)
			return existing, nil
		}
		return core.ArtifactBlobRef{}, err
	}
	stage.path = ""
	if err := r.syncArtifactBlobParent(); err != nil {
		if observed, found, resolveErr := r.resolveExistingArtifactCommit(request); found && resolveErr == nil {
			r.consumeBlobReservation(request.Reservation)
			r.addStateBytes(stage.contentBytes + stage.metadataBytes)
			return observed, nil
		}
		return core.ArtifactBlobRef{}, err
	}
	observed, err := r.resolveArtifactBlobUnlocked(request.BlobID)
	if err != nil || !reflect.DeepEqual(observed, stage.ref) {
		if err == nil {
			err = fmt.Errorf("artifact blob post-commit validation mismatch")
		}
		return core.ArtifactBlobRef{}, err
	}
	r.consumeBlobReservation(request.Reservation)
	r.addStateBytes(stage.contentBytes + stage.metadataBytes)
	return observed, nil
}

func (r *Repository) syncArtifactBlobParent() error {
	parent, err := os.Open(r.artifactBlobRoot())
	if err != nil {
		return err
	}
	syncErr := r.writer.checkpoint("artifact_blob.parent_sync")
	if syncErr == nil {
		syncErr = parent.Sync()
	}
	closeErr := parent.Close()
	if syncErr == nil {
		syncErr = closeErr
	}
	return syncErr
}
