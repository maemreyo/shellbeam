package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ReserveTypedIntent(ctx context.Context, want operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, app.StoreResult) {
	unlock := r.lock(want.OperationID)
	defer unlock()
	r.admit.Lock()
	defer r.admit.Unlock()

	if want.CreatedAt.IsZero() {
		want.CreatedAt = r.now().UTC()
	}
	if err := want.Validate(); err != nil {
		return operation.TypedIntentClaim{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.typedIntentPath(want.OperationID)
	var existing operation.TypedIntentClaim
	if err := readStrict(path, &existing); err == nil {
		return replayTypedIntent(want, existing)
	} else if !errors.Is(err, ErrNotFound) {
		return operation.TypedIntentClaim{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	_, used, err := r.admissionCounters()
	if err != nil {
		return operation.TypedIntentClaim{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if used+r.limits.ControlReserve > r.limits.MaxTotalState {
		return operation.TypedIntentClaim{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistence_unavailable")}
	}
	result := r.writer.Create(path, want)
	if result.Err == nil {
		return want, true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		if err := readStrict(path, &existing); err != nil {
			return operation.TypedIntentClaim{}, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		return replayTypedIntent(want, existing)
	}
	return operation.TypedIntentClaim{}, false, result
}

func (r *Repository) FindTypedIntent(_ context.Context, id operation.ID) (operation.TypedIntentClaim, bool, error) {
	if _, err := operation.ParseID(string(id)); err != nil {
		return operation.TypedIntentClaim{}, false, err
	}
	var claim operation.TypedIntentClaim
	if err := readStrict(r.typedIntentPath(id), &claim); errors.Is(err, ErrNotFound) {
		return operation.TypedIntentClaim{}, false, nil
	} else if err != nil {
		return operation.TypedIntentClaim{}, false, err
	}
	if err := claim.Validate(); err != nil {
		return operation.TypedIntentClaim{}, false, err
	}
	return claim, true, nil
}

func (r *Repository) CommitTypedBinding(ctx context.Context, id operation.ID, want operation.Reservation) (operation.Reservation, bool, app.StoreResult) {
	claim, found, err := r.FindTypedIntent(ctx, id)
	if err != nil {
		return operation.Reservation{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !found {
		return operation.Reservation{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("typed intent claim not found")}
	}
	if err := validateTypedBindingCommit(claim, id, want); err != nil {
		return operation.Reservation{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if want.ExperimentID != "" {
		stored, _, created, result := r.ReserveExperimentOperation(ctx, want, decisionprotocol.ExperimentExecutionLink{ExperimentID: decisionprotocol.ExperimentID(want.ExperimentID)})
		return stored, created, result
	}
	return r.ReserveOperation(ctx, want)
}

func validateTypedBindingCommit(claim operation.TypedIntentClaim, id operation.ID, want operation.Reservation) error {
	if want.OperationID != id || claim.OperationID != id {
		return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if want.EffectiveRequestFingerprint() != claim.RequestFingerprint {
		return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if !sameVerificationAttempt(claim.Intent.VerificationAttempt, want.VerificationAttempt) {
		return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if want.ProjectCommand == nil || (want.SchemaVersion != 3 && want.SchemaVersion != 4) {
		return fmt.Errorf("typed binding requires schema v3 or persistent v4 reservation")
	}
	if claim.Intent.Persistent != want.Persistent || claim.Intent.SessionName != want.SessionName {
		return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	if want.SchemaVersion == 3 && want.Persistent {
		return fmt.Errorf("schema v3 cannot be persistent")
	}
	if want.SchemaVersion == 4 && !want.Persistent {
		return fmt.Errorf("schema v4 typed binding must be persistent")
	}
	if err := want.ProjectCommand.Validate(); err != nil {
		return err
	}
	if want.WorkspaceID != claim.Intent.WorkspaceID || want.ProjectCommand.CommandID != claim.Intent.ProjectCommandID || want.TTY != claim.Intent.TTY || want.TimeoutMS != claim.Intent.TimeoutMS {
		return failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	return nil
}

func replayTypedIntent(want, existing operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, app.StoreResult) {
	if err := existing.Validate(); err != nil {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if existing.OperationID != want.OperationID || existing.RequestFingerprint != want.RequestFingerprint {
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(existing.OperationID)}, nil)}
	}
	return existing, false, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) typedIntentPath(id operation.ID) string {
	return filepath.Join(r.root, "typed-intents", string(id)+".json")
}
