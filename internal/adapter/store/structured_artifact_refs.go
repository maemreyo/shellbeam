package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	artifactBlobRefSchemaV1       = 1
	artifactBlobTombstoneSchemaV1 = 1
	maxArtifactAuthorityEntries   = 4096
	artifactRetirePrefix          = ".artifact-retire-"
)

type ArtifactBlobState string

const (
	ArtifactBlobRetained    ArtifactBlobState = "retained"
	ArtifactBlobCompacted   ArtifactBlobState = "compacted"
	ArtifactBlobUnavailable ArtifactBlobState = "unavailable"
)

type ArtifactBlobTombstone struct {
	SchemaVersion int               `json:"schema_version"`
	BlobID        string            `json:"blob_id"`
	SHA256        string            `json:"sha256"`
	Size          int64             `json:"size"`
	State         ArtifactBlobState `json:"state"`
}

type ArtifactBlobResolution struct {
	State     ArtifactBlobState
	Ref       core.ArtifactBlobRef
	Tombstone ArtifactBlobTombstone
}

type derivationBlobRef struct {
	SchemaVersion int                  `json:"schema_version"`
	BlobRef       core.ArtifactBlobRef `json:"blob_ref"`
	DerivationKey string               `json:"derivation_key"`
}

func (r derivationBlobRef) Validate() error {
	if r.SchemaVersion != artifactBlobRefSchemaV1 || r.BlobRef.Validate() != nil || !validStructuredKey(r.DerivationKey) {
		return fmt.Errorf("invalid derivation blob ref")
	}
	return nil
}

func (t ArtifactBlobTombstone) Validate() error {
	if t.SchemaVersion != artifactBlobTombstoneSchemaV1 || !validArtifactBlobID(t.BlobID) || !validStructuredKey(t.SHA256) || t.Size < 0 || t.Size > structuredapp.MaxArtifactBlobBytes || t.State != ArtifactBlobCompacted {
		return fmt.Errorf("invalid artifact blob tombstone")
	}
	return nil
}

func (r *Repository) artifactBlobRefRoot() string {
	return filepath.Join(r.structuredRoot(), "artifact-refs")
}
func (r *Repository) artifactBlobTombstoneRoot() string {
	return filepath.Join(r.structuredRoot(), "artifact-blob-tombstones")
}
func (r *Repository) derivationBlobRefPath(blobID, derivationKey string) string {
	return filepath.Join(r.artifactBlobRefRoot(), blobID+"."+derivationKey+".json")
}
func (r *Repository) artifactBlobTombstonePath(blobID string) string {
	return filepath.Join(r.artifactBlobTombstoneRoot(), blobID+".json")
}
func (r *Repository) artifactRetirementPath(blobID string) string {
	return filepath.Join(r.artifactBlobRoot(), artifactRetirePrefix+blobID)
}

func (r *Repository) ResolveArtifactInputState(ctx context.Context, expected core.ArtifactBlobRef) (structuredapp.InputSourceState, error) {
	resolution, err := r.ResolveArtifactBlobState(ctx, expected)
	if err != nil {
		return structuredapp.InputSourceUnavailable, err
	}
	switch resolution.State {
	case ArtifactBlobRetained:
		return structuredapp.InputSourceRetained, nil
	case ArtifactBlobCompacted:
		return structuredapp.InputSourceCompacted, nil
	default:
		return structuredapp.InputSourceUnavailable, nil
	}
}

func (r *Repository) ResolveArtifactBlobState(ctx context.Context, expected core.ArtifactBlobRef) (ArtifactBlobResolution, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactBlobResolution{}, err
	}
	if err := expected.Validate(); err != nil {
		return ArtifactBlobResolution{}, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	return r.resolveArtifactBlobStateUnlocked(expected)
}

func (r *Repository) resolveArtifactBlobStateUnlocked(expected core.ArtifactBlobRef) (ArtifactBlobResolution, error) {
	got, err := r.resolveArtifactBlobUnlocked(expected.BlobID)
	if err == nil {
		if _, tombstoneErr := r.readArtifactBlobTombstoneUnlocked(expected.BlobID); tombstoneErr == nil {
			return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, fmt.Errorf("artifact blob retained/tombstone conflict")
		} else if !errors.Is(tombstoneErr, ErrNotFound) {
			return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, tombstoneErr
		}
		if !reflect.DeepEqual(got, expected) {
			return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, fmt.Errorf("artifact blob identity conflict")
		}
		return ArtifactBlobResolution{State: ArtifactBlobRetained, Ref: got}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, err
	}
	var tombstone ArtifactBlobTombstone
	if err := readPrivateJSON(r.artifactBlobTombstonePath(expected.BlobID), maxStructuredMetadataBytes, &tombstone); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, nil
		}
		return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, err
	}
	if err := tombstone.Validate(); err != nil || tombstone.BlobID != expected.BlobID || tombstone.SHA256 != expected.SHA256 || tombstone.Size != expected.Size {
		return ArtifactBlobResolution{State: ArtifactBlobUnavailable}, fmt.Errorf("artifact blob tombstone mismatch")
	}
	return ArtifactBlobResolution{State: ArtifactBlobCompacted, Tombstone: tombstone}, nil
}

func (r *Repository) AcquireDerivationBlobRefs(ctx context.Context, derivation core.Derivation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStructuredDerivation(derivation); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	_, err := r.acquireDerivationBlobRefsUnlocked(derivation)
	return err
}

func (r *Repository) acquireDerivationBlobRefsUnlocked(derivation core.Derivation) ([]string, error) {
	created := make([]string, 0, len(derivation.SourceAuthorityRefs))
	for _, input := range derivation.SourceAuthorityRefs {
		if input.Kind != core.StructuredInputArtifactBlob || input.ArtifactBlob == nil {
			continue
		}
		state, err := r.resolveArtifactBlobStateUnlocked(*input.ArtifactBlob)
		if err != nil || state.State != ArtifactBlobRetained {
			if err == nil {
				err = fmt.Errorf("artifact_blob_unavailable")
			}
			r.rollbackDerivationBlobRefsUnlocked(created)
			return nil, err
		}
		want := derivationBlobRef{SchemaVersion: artifactBlobRefSchemaV1, BlobRef: *input.ArtifactBlob, DerivationKey: derivation.DerivationKey}
		path := r.derivationBlobRefPath(want.BlobRef.BlobID, want.DerivationKey)
		var current derivationBlobRef
		if err := readPrivateJSON(path, maxStructuredMetadataBytes, &current); err == nil {
			if current.Validate() != nil || !reflect.DeepEqual(current, want) {
				r.rollbackDerivationBlobRefsUnlocked(created)
				return nil, fmt.Errorf("artifact_blob_ref_conflict")
			}
			continue
		} else if !errors.Is(err, ErrNotFound) {
			r.rollbackDerivationBlobRefsUnlocked(created)
			return nil, err
		}
		if result := r.writer.Create(path, want); result.Err != nil {
			r.rollbackDerivationBlobRefsUnlocked(created)
			return nil, result.Err
		}
		created = append(created, path)
	}
	return created, nil
}

func (r *Repository) rollbackDerivationBlobRefsUnlocked(paths []string) {
	changed := false
	for _, path := range paths {
		if err := os.Remove(path); err == nil {
			changed = true
		}
	}
	if changed {
		_ = syncPrivateDirectory(r.artifactBlobRefRoot())
	}
}

func (r *Repository) releaseDerivationBlobRefsUnlocked(derivation core.Derivation) ([]core.ArtifactBlobRef, error) {
	artifacts := artifactRefsForDerivation(derivation)
	changed := false
	for _, ref := range artifacts {
		path := r.derivationBlobRefPath(ref.BlobID, derivation.DerivationKey)
		var stored derivationBlobRef
		if err := readPrivateJSON(path, maxStructuredMetadataBytes, &stored); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if stored.Validate() != nil || stored.DerivationKey != derivation.DerivationKey || !reflect.DeepEqual(stored.BlobRef, ref) {
			return nil, fmt.Errorf("artifact_blob_ref_conflict")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		changed = true
	}
	if changed {
		if err := syncPrivateDirectory(r.artifactBlobRefRoot()); err != nil {
			return nil, err
		}
	}
	return artifacts, nil
}

func artifactRefsForDerivation(derivation core.Derivation) []core.ArtifactBlobRef {
	refs := make([]core.ArtifactBlobRef, 0, len(derivation.SourceAuthorityRefs))
	for _, input := range derivation.SourceAuthorityRefs {
		if input.Kind == core.StructuredInputArtifactBlob && input.ArtifactBlob != nil {
			refs = append(refs, *input.ArtifactBlob)
		}
	}
	return refs
}

func (r *Repository) releaseRecoveryClaimsForArtifactsUnlocked(refs []core.ArtifactBlobRef) error {
	if len(refs) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		wanted[ref.BlobID] = struct{}{}
	}
	entries, err := boundedPrivateEntries(r.artifactRecoveryRoot())
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected artifact recovery entry")
		}
		captureID := strings.TrimSuffix(entry.Name(), ".json")
		claim, err := readArtifactRecoveryClaim(r.artifactRecoveryPath(captureID))
		if err != nil {
			return err
		}
		if _, ok := wanted[claim.BlobID]; !ok {
			continue
		}
		if err := os.Remove(r.artifactRecoveryPath(captureID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		changed = true
	}
	if changed {
		return syncPrivateDirectory(r.artifactRecoveryRoot())
	}
	return nil
}

func boundedPrivateEntries(root string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxArtifactAuthorityEntries {
		return nil, fmt.Errorf("artifact authority scan limit exceeded")
	}
	return entries, nil
}

func syncPrivateDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func (r *Repository) artifactBlobHasLiveRefsUnlocked(blobID string) (bool, error) {
	entries, err := boundedPrivateEntries(r.artifactBlobRefRoot())
	if err != nil {
		return false, err
	}
	prefix := blobID + "."
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return false, fmt.Errorf("unexpected artifact ref entry")
		}
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		var ref derivationBlobRef
		if err := readPrivateJSON(filepath.Join(r.artifactBlobRefRoot(), entry.Name()), maxStructuredMetadataBytes, &ref); err != nil || ref.Validate() != nil || ref.BlobRef.BlobID != blobID {
			return false, fmt.Errorf("invalid artifact blob ref authority")
		}
		return true, nil
	}
	return false, nil
}

func (r *Repository) artifactBlobHasRecoveryClaimUnlocked(blobID string) (bool, error) {
	entries, err := boundedPrivateEntries(r.artifactRecoveryRoot())
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return false, fmt.Errorf("unexpected artifact recovery entry")
		}
		captureID := strings.TrimSuffix(entry.Name(), ".json")
		claim, err := readArtifactRecoveryClaim(r.artifactRecoveryPath(captureID))
		if err != nil {
			return false, err
		}
		if claim.BlobID == blobID {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repository) retireArtifactBlobIfUnownedUnlocked(ref core.ArtifactBlobRef) (ArtifactBlobState, error) {
	if err := ref.Validate(); err != nil {
		return ArtifactBlobUnavailable, err
	}
	live, err := r.artifactBlobHasLiveRefsUnlocked(ref.BlobID)
	if err != nil || live {
		return ArtifactBlobRetained, err
	}
	claimed, err := r.artifactBlobHasRecoveryClaimUnlocked(ref.BlobID)
	if err != nil || claimed {
		return ArtifactBlobRetained, err
	}
	if tombstone, err := r.readArtifactBlobTombstoneUnlocked(ref.BlobID); err == nil {
		if tombstone.SHA256 != ref.SHA256 || tombstone.Size != ref.Size {
			return ArtifactBlobUnavailable, fmt.Errorf("artifact blob tombstone conflict")
		}
		return ArtifactBlobCompacted, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ArtifactBlobUnavailable, err
	}
	retained, err := r.resolveArtifactBlobUnlocked(ref.BlobID)
	if err != nil {
		return ArtifactBlobUnavailable, err
	}
	if !reflect.DeepEqual(retained, ref) {
		return ArtifactBlobUnavailable, fmt.Errorf("artifact blob retirement identity mismatch")
	}
	return r.withdrawArtifactBlobUnlocked(ref)
}

func (r *Repository) withdrawArtifactBlobUnlocked(ref core.ArtifactBlobRef) (ArtifactBlobState, error) {
	finalPath := r.artifactBlobPath(ref.BlobID)
	staged := r.artifactRetirementPath(ref.BlobID)
	if _, err := os.Lstat(staged); err == nil {
		return ArtifactBlobUnavailable, fmt.Errorf("artifact retirement staging conflict")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ArtifactBlobUnavailable, err
	}
	freed := directorySize(finalPath)
	if err := os.Rename(finalPath, staged); err != nil {
		return ArtifactBlobUnavailable, err
	}
	if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
		_ = os.Rename(staged, finalPath)
		_ = syncPrivateDirectory(r.artifactBlobRoot())
		return ArtifactBlobUnavailable, err
	}
	tombstone := ArtifactBlobTombstone{SchemaVersion: artifactBlobTombstoneSchemaV1, BlobID: ref.BlobID, SHA256: ref.SHA256, Size: ref.Size, State: ArtifactBlobCompacted}
	if err := r.persistArtifactBlobTombstoneUnlocked(tombstone); err != nil {
		_ = os.Rename(staged, finalPath)
		_ = syncPrivateDirectory(r.artifactBlobRoot())
		return ArtifactBlobUnavailable, err
	}
	if err := os.RemoveAll(staged); err != nil {
		return ArtifactBlobCompacted, err
	}
	if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
		return ArtifactBlobCompacted, err
	}
	if freed > 0 {
		r.addStateBytes(-freed)
	}
	return ArtifactBlobCompacted, nil
}

func (r *Repository) persistArtifactBlobTombstoneUnlocked(tombstone ArtifactBlobTombstone) error {
	if err := tombstone.Validate(); err != nil {
		return err
	}
	path := r.artifactBlobTombstonePath(tombstone.BlobID)
	current, err := r.readArtifactBlobTombstoneUnlocked(tombstone.BlobID)
	if err == nil {
		if reflect.DeepEqual(current, tombstone) {
			return nil
		}
		return fmt.Errorf("artifact blob tombstone conflict")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	result := r.writer.Create(path, tombstone)
	if result.Err == nil {
		return nil
	}
	if observed, readErr := r.readArtifactBlobTombstoneUnlocked(tombstone.BlobID); readErr == nil && reflect.DeepEqual(observed, tombstone) {
		return nil
	}
	return result.Err
}

func (r *Repository) readArtifactBlobTombstoneUnlocked(blobID string) (ArtifactBlobTombstone, error) {
	var tombstone ArtifactBlobTombstone
	if err := readPrivateJSON(r.artifactBlobTombstonePath(blobID), maxStructuredMetadataBytes, &tombstone); err != nil {
		return tombstone, err
	}
	return tombstone, tombstone.Validate()
}
