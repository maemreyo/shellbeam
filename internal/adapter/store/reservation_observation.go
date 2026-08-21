package store

import (
	"context"
	"fmt"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func validateReservationObservationMetadata(v operation.Reservation) error {
	if v.StructuredCaptureDigest != "" && (!operation.ValidStructuredCaptureDigest(v.StructuredCaptureDigest) || v.ObservationBindingFingerprint == "") {
		return fmt.Errorf("invalid reservation structured capture digest")
	}
	return validateReservationVerificationAttempt(v.VerificationAttempt)
}

func (r *Repository) validateVerificationAttemptAuthority(ctx context.Context, v operation.Reservation) error {
	if v.StructuredCaptureDigest != "" && v.ProjectCommand == nil {
		fingerprint, err := (operation.ObservationBinding{
			ActivityID: v.ActivityID, ExperimentID: v.ExperimentID, Intent: v.Intent, StructuredAdapter: v.StructuredAdapter, StructuredCaptureDigest: v.StructuredCaptureDigest,
			Evidence: v.Evidence, VerificationAttempt: v.VerificationAttempt,
		}).Fingerprint()
		if err != nil {
			return err
		}
		if fingerprint != v.ObservationBindingFingerprint {
			return failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(v.OperationID), "field": "structured_capture_digest"}, nil)
		}
	}
	if v.VerificationAttempt == nil {
		return nil
	}
	if err := v.VerificationAttempt.Validate(); err != nil {
		return fmt.Errorf("invalid reservation verification attempt: %w", err)
	}
	if v.ProjectCommand != nil {
		claim, found, err := r.FindTypedIntent(ctx, v.OperationID)
		if err != nil {
			return err
		}
		if !found || claim.RequestFingerprint != v.EffectiveRequestFingerprint() || !sameVerificationAttempt(claim.Intent.VerificationAttempt, v.VerificationAttempt) {
			return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(v.OperationID)}, nil)
		}
		return nil
	}
	if v.Evidence == nil {
		return fmt.Errorf("raw verification attempt requires evidence contract")
	}
	fingerprint, err := (operation.ObservationBinding{
		ActivityID: v.ActivityID, ExperimentID: v.ExperimentID, Intent: v.Intent, StructuredAdapter: v.StructuredAdapter, StructuredCaptureDigest: v.StructuredCaptureDigest,
		Evidence: v.Evidence, VerificationAttempt: v.VerificationAttempt,
	}).Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != v.ObservationBindingFingerprint {
		return failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(v.OperationID)}, nil)
	}
	return nil
}

func sameVerificationAttempt(a, b *evidence.VerificationAttemptIntent) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
