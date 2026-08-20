package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ReserveCaptureAuthority(ctx context.Context, authority structuredapp.CaptureAuthority) (structuredapp.CaptureAuthorityRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.CaptureAuthorityRecord{}, false, err
	}
	record, err := structuredapp.NewCaptureAuthorityRecord(authority)
	if err != nil {
		return structuredapp.CaptureAuthorityRecord{}, false, err
	}
	operationID, err := operation.ParseID(authority.Intent.OperationID)
	if err != nil {
		return structuredapp.CaptureAuthorityRecord{}, false, err
	}

	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	path := r.captureAuthorityPath(operationID)
	current, err := readCaptureAuthorityRecord(path)
	if err == nil {
		if reflect.DeepEqual(current.Authority, record.Authority) && current.StructuredCaptureDigest == record.StructuredCaptureDigest {
			return current, false, nil
		}
		return current, false, fmt.Errorf("structured_capture_authority_conflict")
	}
	if !errors.Is(err, ErrNotFound) {
		return structuredapp.CaptureAuthorityRecord{}, false, err
	}
	result := r.writer.Create(path, record)
	if result.Err != nil {
		// A concurrent or ambiguously acknowledged create resolves only through
		// the deterministic destination.
		if current, readErr := readCaptureAuthorityRecord(path); readErr == nil {
			if reflect.DeepEqual(current.Authority, record.Authority) && current.StructuredCaptureDigest == record.StructuredCaptureDigest {
				return current, false, nil
			}
			return current, false, fmt.Errorf("structured_capture_authority_conflict")
		}
		return structuredapp.CaptureAuthorityRecord{}, false, result.Err
	}
	return record, true, nil
}

func (r *Repository) MarkCaptureAuthorityState(ctx context.Context, id operation.ID, state structuredapp.CaptureAuthorityState) (structuredapp.CaptureAuthorityRecord, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.CaptureAuthorityRecord{}, err
	}
	if _, err := operation.ParseID(string(id)); err != nil {
		return structuredapp.CaptureAuthorityRecord{}, err
	}
	if state != structuredapp.CaptureAuthorityManagedPathCollision && state != structuredapp.CaptureAuthorityAbandoned {
		return structuredapp.CaptureAuthorityRecord{}, fmt.Errorf("invalid capture authority transition")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	path := r.captureAuthorityPath(id)
	current, err := readCaptureAuthorityRecord(path)
	if err != nil {
		return current, err
	}
	if current.State == state {
		return current, nil
	}
	if current.State != structuredapp.CaptureAuthorityPrepared {
		return current, fmt.Errorf("capture_authority_state_conflict")
	}
	next := current
	next.State = state
	if err := next.Validate(); err != nil {
		return current, err
	}
	result := r.writer.Replace(path, next)
	if result.Err == nil {
		return next, nil
	}
	if observed, readErr := readCaptureAuthorityRecord(path); readErr == nil && reflect.DeepEqual(observed, next) {
		return observed, nil
	}
	return current, result.Err
}

func (r *Repository) FindCaptureAuthority(ctx context.Context, id operation.ID) (structuredapp.CaptureAuthorityRecord, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.CaptureAuthorityRecord{}, err
	}
	if _, err := operation.ParseID(string(id)); err != nil {
		return structuredapp.CaptureAuthorityRecord{}, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	record, err := readCaptureAuthorityRecord(r.captureAuthorityPath(id))
	if errors.Is(err, ErrNotFound) {
		return record, structuredapp.ErrCaptureAuthorityNotFound
	}
	return record, err
}

func readCaptureAuthorityRecord(path string) (structuredapp.CaptureAuthorityRecord, error) {
	var record structuredapp.CaptureAuthorityRecord
	if err := readPrivateJSON(path, maxStructuredMetadataBytes, &record); err != nil {
		return record, err
	}
	if err := record.Validate(); err != nil {
		return record, err
	}
	return record, nil
}
