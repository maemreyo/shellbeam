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

func (r *Repository) RecoverStructuredArtifacts(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	if err := r.validateArtifactBlobStoreUnlocked(); err != nil {
		return err
	}
	if err := r.validateArtifactTombstoneStoreUnlocked(); err != nil {
		return err
	}
	if err := r.reconcileArtifactRetirementsUnlocked(); err != nil {
		return err
	}
	if err := r.validateArtifactRecoveryClaimsUnlocked(); err != nil {
		return err
	}
	if err := r.reconcileDerivationBlobRefsUnlocked(); err != nil {
		return err
	}
	if err := r.recoverStructuredDerivationsUnlocked(); err != nil {
		return err
	}
	return r.collectArtifactOrphansUnlocked()
}

func (r *Repository) CollectArtifactOrphans(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	if err := r.reconcileArtifactRetirementsUnlocked(); err != nil {
		return err
	}
	return r.collectArtifactOrphansUnlocked()
}

func (r *Repository) validateArtifactBlobStoreUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactBlobRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, ".artifact-stage-"):
			continue
		case strings.HasPrefix(name, artifactRetirePrefix):
			if !entry.IsDir() || !validArtifactBlobID(strings.TrimPrefix(name, artifactRetirePrefix)) {
				return fmt.Errorf("invalid artifact retirement staging")
			}
			continue
		case validArtifactBlobID(name):
			if !entry.IsDir() {
				return fmt.Errorf("artifact blob authority is not directory")
			}
			if _, err := r.resolveArtifactBlobUnlocked(name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected artifact blob entry")
		}
	}
	return nil
}

func (r *Repository) validateArtifactTombstoneStoreUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactBlobTombstoneRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected artifact tombstone entry")
		}
		blobID := strings.TrimSuffix(entry.Name(), ".json")
		tombstone, err := r.readArtifactBlobTombstoneUnlocked(blobID)
		if err != nil || tombstone.BlobID != blobID {
			return fmt.Errorf("invalid artifact tombstone authority: %w", err)
		}
		if _, statErr := os.Lstat(r.artifactBlobPath(blobID)); statErr == nil {
			return fmt.Errorf("artifact retained/tombstone conflict")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	return nil
}

func (r *Repository) reconcileArtifactRetirementsUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactBlobRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), artifactRetirePrefix) {
			continue
		}
		blobID := strings.TrimPrefix(entry.Name(), artifactRetirePrefix)
		if !entry.IsDir() || !validArtifactBlobID(blobID) {
			return fmt.Errorf("invalid artifact retirement staging")
		}
		if _, err := os.Lstat(r.artifactBlobPath(blobID)); err == nil {
			return fmt.Errorf("artifact retirement final/staging conflict")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		claimed, err := r.artifactBlobHasRecoveryClaimUnlocked(blobID)
		if err != nil {
			return err
		}
		if claimed {
			return fmt.Errorf("artifact retirement conflicts with recovery claim")
		}
		staged := r.artifactRetirementPath(blobID)
		metadata, err := readArtifactBlobMetadata(staged)
		if err != nil || metadata.Ref.BlobID != blobID {
			return fmt.Errorf("invalid artifact retirement source: %w", err)
		}
		if err := readArtifactBlobContent(staged, metadata.Ref); err != nil {
			return err
		}
		freed := directorySize(staged)
		tombstone := ArtifactBlobTombstone{SchemaVersion: artifactBlobTombstoneSchemaV1, BlobID: blobID, SHA256: metadata.Ref.SHA256, Size: metadata.Ref.Size, State: ArtifactBlobCompacted}
		if err := r.persistArtifactBlobTombstoneUnlocked(tombstone); err != nil {
			return err
		}
		if err := os.RemoveAll(staged); err != nil {
			return err
		}
		if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
			return err
		}
		if freed > 0 {
			r.addStateBytes(-freed)
		}
	}
	return nil
}

func (r *Repository) validateArtifactRecoveryClaimsUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactRecoveryRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected artifact recovery entry")
		}
		captureID := strings.TrimSuffix(entry.Name(), ".json")
		claim, err := readArtifactRecoveryClaim(r.artifactRecoveryPath(captureID))
		if err != nil {
			return err
		}
		if err := r.validateRecoveryClaimBlobUnlocked(claim); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) validateRecoveryClaimBlobUnlocked(claim structuredapp.ArtifactRecoveryClaim) error {
	metadata, err := readArtifactBlobMetadata(r.artifactBlobPath(claim.BlobID))
	if err == nil {
		if metadata.CaptureIntentDigest != claim.CaptureAuthorityID || metadata.Ref.BlobID != claim.BlobID ||
			metadata.Ref.OperationID != claim.OperationID || metadata.Ref.SessionID != claim.SessionID ||
			metadata.Ref.RepositoryID != claim.RepositoryID || metadata.Ref.WorkspaceID != claim.WorkspaceID || metadata.Ref.TerminalCut != claim.TerminalCut {
			return fmt.Errorf("artifact recovery claim/blob mismatch")
		}
		return readArtifactBlobContent(r.artifactBlobPath(claim.BlobID), metadata.Ref)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, tombstoneErr := r.readArtifactBlobTombstoneUnlocked(claim.BlobID); tombstoneErr == nil {
		return fmt.Errorf("compacted blob retains recovery claim")
	} else if !errors.Is(tombstoneErr, ErrNotFound) {
		return tombstoneErr
	}
	return nil
}

func (r *Repository) reconcileDerivationBlobRefsUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactBlobRefRoot())
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected artifact ref entry")
		}
		path := filepath.Join(r.artifactBlobRefRoot(), entry.Name())
		var ref derivationBlobRef
		if err := readPrivateJSON(path, maxStructuredMetadataBytes, &ref); err != nil || ref.Validate() != nil {
			return fmt.Errorf("invalid artifact blob ref authority")
		}
		derivation, err := r.readDerivationUnlocked(ref.DerivationKey)
		if errors.Is(err, ErrNotFound) || err == nil && derivation.Completeness == core.CompletenessCompacted {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
			changed = true
			continue
		}
		if err != nil {
			return err
		}
		if !derivationContainsArtifactRef(derivation, ref.BlobRef) {
			return fmt.Errorf("artifact ref does not belong to derivation")
		}
	}
	if changed {
		return syncPrivateDirectory(r.artifactBlobRefRoot())
	}
	return nil
}

func (r *Repository) recoverStructuredDerivationsUnlocked() error {
	entries, err := boundedPrivateEntries(r.structuredDerivationDir())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected structured derivation entry")
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		derivation, err := r.readDerivationUnlocked(key)
		if err != nil {
			return err
		}
		artifacts := artifactRefsForDerivation(derivation)
		if len(artifacts) == 0 {
			continue
		}
		if derivation.Completeness == core.CompletenessCompacted {
			if _, err := r.releaseDerivationBlobRefsUnlocked(derivation); err != nil {
				return err
			}
			if err := r.releaseRecoveryClaimsForArtifactsUnlocked(artifacts); err != nil {
				return err
			}
			for _, ref := range artifacts {
				if _, err := r.retireArtifactBlobIfUnownedUnlocked(ref); err != nil {
					return err
				}
			}
			continue
		}
		if _, err := r.acquireDerivationBlobRefsUnlocked(derivation); err != nil {
			return err
		}
		if err := r.releaseRecoveryClaimsForArtifactsUnlocked(artifacts); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) collectArtifactOrphansUnlocked() error {
	entries, err := boundedPrivateEntries(r.artifactBlobRoot())
	if err != nil {
		return err
	}
	stagingChanged := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".artifact-stage-") {
			continue
		}
		path := filepath.Join(r.artifactBlobRoot(), entry.Name())
		freed := directorySize(path)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if freed > 0 {
			r.addStateBytes(-freed)
		}
		stagingChanged = true
	}
	if stagingChanged {
		if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
			return err
		}
	}
	entries, err = boundedPrivateEntries(r.artifactBlobRoot())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !validArtifactBlobID(entry.Name()) {
			if strings.HasPrefix(entry.Name(), artifactRetirePrefix) {
				return fmt.Errorf("unreconciled artifact retirement staging")
			}
			if strings.HasPrefix(entry.Name(), ".artifact-stage-") {
				continue
			}
			return fmt.Errorf("unexpected artifact blob entry")
		}
		ref, err := r.resolveArtifactBlobUnlocked(entry.Name())
		if err != nil {
			return err
		}
		if _, err := r.retireArtifactBlobIfUnownedUnlocked(ref); err != nil {
			return err
		}
	}
	return nil
}

func derivationContainsArtifactRef(derivation core.Derivation, ref core.ArtifactBlobRef) bool {
	for _, candidate := range artifactRefsForDerivation(derivation) {
		if reflect.DeepEqual(candidate, ref) {
			return true
		}
	}
	return false
}
