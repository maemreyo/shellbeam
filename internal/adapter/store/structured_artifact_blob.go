package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync/atomic"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func (r *Repository) ReserveBlobBytes(ctx context.Context, captureID string, bytes int64) (structuredapp.BlobByteReservation, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.BlobByteReservation{}, err
	}
	want := structuredapp.BlobByteReservation{CaptureAuthorityID: captureID, Bytes: bytes}
	if err := want.Validate(); err != nil {
		return structuredapp.BlobByteReservation{}, err
	}
	r.blobBudgetMu.Lock()
	defer r.blobBudgetMu.Unlock()
	if current, ok := r.blobReservations[captureID]; ok {
		if current == bytes {
			return want, nil
		}
		return structuredapp.BlobByteReservation{}, fmt.Errorf("blob_byte_reservation_conflict")
	}
	exact, err := r.scanStateBytes()
	if err != nil {
		return structuredapp.BlobByteReservation{}, err
	}
	outstanding := int64(0)
	for _, reserved := range r.blobReservations {
		if reserved > math.MaxInt64-outstanding {
			return structuredapp.BlobByteReservation{}, structuredapp.ErrArtifactSourceBudgetExceeded
		}
		outstanding += reserved
	}
	if r.limits.MaxTotalState <= r.limits.ControlReserve || exact > r.limits.MaxTotalState-r.limits.ControlReserve ||
		outstanding > r.limits.MaxTotalState-r.limits.ControlReserve-exact || bytes > r.limits.MaxTotalState-r.limits.ControlReserve-exact-outstanding {
		return structuredapp.BlobByteReservation{}, structuredapp.ErrArtifactSourceBudgetExceeded
	}
	r.blobReservations[captureID] = bytes
	return want, nil
}

func (r *Repository) ReleaseBlobReservation(ctx context.Context, reservation structuredapp.BlobByteReservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reservation.Validate(); err != nil {
		return err
	}
	r.blobBudgetMu.Lock()
	defer r.blobBudgetMu.Unlock()
	current, ok := r.blobReservations[reservation.CaptureAuthorityID]
	if !ok {
		return nil
	}
	if current != reservation.Bytes {
		return fmt.Errorf("blob_byte_reservation_conflict")
	}
	delete(r.blobReservations, reservation.CaptureAuthorityID)
	return nil
}

func (r *Repository) PutRecoveryClaim(ctx context.Context, claim structuredapp.ArtifactRecoveryClaim) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	path := r.artifactRecoveryPath(claim.CaptureAuthorityID)
	current, err := readArtifactRecoveryClaim(path)
	if err == nil {
		if reflect.DeepEqual(current, claim) {
			return nil
		}
		return fmt.Errorf("artifact_recovery_claim_conflict")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := r.writer.checkpoint("artifact_recovery_claim.write"); err != nil {
		return err
	}
	result := r.writer.Create(path, claim)
	if result.Err == nil {
		return nil
	}
	if observed, readErr := readArtifactRecoveryClaim(path); readErr == nil {
		if reflect.DeepEqual(observed, claim) {
			return nil
		}
		return fmt.Errorf("artifact_recovery_claim_conflict")
	}
	return result.Err
}

func (r *Repository) GetRecoveryClaim(ctx context.Context, captureID string) (structuredapp.ArtifactRecoveryClaim, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.ArtifactRecoveryClaim{}, err
	}
	if !validStructuredKey(captureID) {
		return structuredapp.ArtifactRecoveryClaim{}, fmt.Errorf("invalid capture authority id")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	return readArtifactRecoveryClaim(r.artifactRecoveryPath(captureID))
}

func readArtifactRecoveryClaim(path string) (structuredapp.ArtifactRecoveryClaim, error) {
	var claim structuredapp.ArtifactRecoveryClaim
	if err := readPrivateJSON(path, maxStructuredMetadataBytes, &claim); err != nil {
		return claim, err
	}
	return claim, claim.Validate()
}

func (r *Repository) ResolveArtifactBlob(ctx context.Context, blobID string) (core.ArtifactBlobRef, error) {
	if err := ctx.Err(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	return r.resolveArtifactBlobUnlocked(blobID)
}

func (r *Repository) CommitArtifactBlob(ctx context.Context, request structuredapp.ArtifactBlobCommitRequest) (core.ArtifactBlobRef, error) {
	if err := ctx.Err(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if err := request.Validate(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	claim, err := readArtifactRecoveryClaim(r.artifactRecoveryPath(request.CaptureAuthorityID))
	if err != nil {
		return core.ArtifactBlobRef{}, fmt.Errorf("artifact recovery claim required: %w", err)
	}
	if !recoveryClaimMatchesCommit(claim, request) {
		return core.ArtifactBlobRef{}, fmt.Errorf("artifact recovery claim mismatch")
	}
	if !r.blobReservationActive(request.Reservation) {
		return core.ArtifactBlobRef{}, fmt.Errorf("artifact blob byte reservation unavailable")
	}
	if existing, found, err := r.resolveExistingArtifactCommit(request); found || err != nil {
		if err == nil {
			r.consumeBlobReservation(request.Reservation)
		}
		return existing, err
	}
	stage, err := r.stageArtifactBlob(request)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	defer os.RemoveAll(stage.path)
	return r.publishArtifactBlobStage(request, stage)
}

func recoveryClaimMatchesCommit(claim structuredapp.ArtifactRecoveryClaim, request structuredapp.ArtifactBlobCommitRequest) bool {
	return claim.Validate() == nil && claim.CaptureAuthorityID == request.CaptureAuthorityID && claim.BlobID == request.BlobID &&
		claim.OperationID == request.Intent.OperationID && claim.SessionID == request.Intent.SessionID && claim.RepositoryID == request.Intent.RepositoryID &&
		claim.WorkspaceID == request.Intent.WorkspaceID && claim.AdapterID == request.Intent.AdapterID && claim.TerminalCut == request.TerminalCut
}

func (r *Repository) resolveExistingArtifactCommit(request structuredapp.ArtifactBlobCommitRequest) (core.ArtifactBlobRef, bool, error) {
	path := r.artifactBlobPath(request.BlobID)
	_, statErr := os.Lstat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return core.ArtifactBlobRef{}, false, nil
	}
	if statErr != nil {
		return core.ArtifactBlobRef{}, true, statErr
	}
	metadata, err := readArtifactBlobMetadata(path)
	if err != nil || !sameArtifactBlobCommit(metadata, request) {
		if err == nil {
			err = fmt.Errorf("artifact_blob_conflict")
		}
		return core.ArtifactBlobRef{}, true, err
	}
	ref, err := r.resolveArtifactBlobUnlocked(request.BlobID)
	return ref, true, err
}

func (r *Repository) blobReservationActive(reservation structuredapp.BlobByteReservation) bool {
	if reservation.Validate() != nil {
		return false
	}
	r.blobBudgetMu.Lock()
	defer r.blobBudgetMu.Unlock()
	return r.blobReservations[reservation.CaptureAuthorityID] == reservation.Bytes
}

func (r *Repository) consumeBlobReservation(reservation structuredapp.BlobByteReservation) {
	r.blobBudgetMu.Lock()
	defer r.blobBudgetMu.Unlock()
	if r.blobReservations[reservation.CaptureAuthorityID] == reservation.Bytes {
		delete(r.blobReservations, reservation.CaptureAuthorityID)
	}
}

func (r *Repository) outstandingBlobReservationBytes() int64 {
	r.blobBudgetMu.Lock()
	defer r.blobBudgetMu.Unlock()
	total := int64(0)
	for _, reserved := range r.blobReservations {
		if reserved > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += reserved
	}
	return total
}

func addStateAuthorityBytes(actual, reserved int64) int64 {
	if actual < 0 || reserved < 0 || reserved > math.MaxInt64-actual {
		return math.MaxInt64
	}
	return actual + reserved
}

type blobBudgetEligibility struct{ released atomic.Bool }

func (c *blobBudgetEligibility) Release() error { c.released.Store(true); return nil }

func (r *Repository) AcquireBlobBudgetCapability(ctx context.Context, captureID string, maxBytes int64) (structuredapp.BlobBudgetCapability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validStructuredKey(captureID) || maxBytes < 1 || maxBytes > structuredapp.MaxArtifactBlobBytes {
		return nil, structuredapp.ErrArtifactSourceBudgetExceeded
	}
	return &blobBudgetEligibility{}, nil
}

func validArtifactBlobID(value string) bool {
	if !strings.HasPrefix(value, "abl_") || len(value) != 4+64 {
		return false
	}
	_, err := hex.DecodeString(value[4:])
	return err == nil
}
