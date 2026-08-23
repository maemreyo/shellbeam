package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	contextExecStoreSchemaVersion       = 2
	maxContextExecRecordBytes     int64 = 512 << 10
)

type contextExecRecord struct {
	SchemaVersion       int                        `json:"schema_version"`
	State               operation.ContextExecState `json:"state"`
	ClaimVerifierDigest string                     `json:"claim_verifier_digest,omitempty"`
}

func (v contextExecRecord) validate() error {
	if v.SchemaVersion != contextExecStoreSchemaVersion {
		return fmt.Errorf("invalid context exec store schema")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if v.ClaimVerifierDigest != "" && !validContextExecVerifier(v.ClaimVerifierDigest) {
		return fmt.Errorf("invalid context exec claim verifier")
	}
	switch v.State.Lifecycle {
	case contextexec.LifecycleHelperAuthenticated, contextexec.LifecycleChildReserved, contextexec.LifecycleChildSpawned, contextexec.LifecycleChildTerminal, contextexec.LifecycleCanonicalized:
		if v.ClaimVerifierDigest == "" {
			return fmt.Errorf("authenticated context exec lacks claim verifier")
		}
	}
	return nil
}

func (r *Repository) ReserveContextExec(ctx context.Context, want operation.ContextExecState) (operation.ContextExecState, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if want.Lifecycle != contextexec.LifecycleReserved {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("context exec reservation must start reserved")}
	}
	if want.CreatedAt.IsZero() {
		want.CreatedAt = r.now().UTC()
	}
	if want.UpdatedAt.IsZero() {
		want.UpdatedAt = want.CreatedAt
	}
	if err := want.Validate(); err != nil {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}

	unlock := r.lock(operation.ID(want.Request.ContextExecID))
	defer unlock()
	r.admit.Lock()
	defer r.admit.Unlock()

	path := r.contextExecPath(want.Request.ContextExecID)
	var existing contextExecRecord
	if _, err := os.Stat(r.legacyContextExecPath(want.Request.ContextExecID)); err == nil {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.DurableChange, Err: legacyContextExecError(want.Request.ContextExecID, want.Request.SessionID)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := readPrivateJSON(path, maxContextExecRecordBytes, &existing); err == nil {
		return replayContextExecReserve(want, existing)
	} else if !errors.Is(err, ErrNotFound) {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	_, used, err := r.admissionCounters()
	if err != nil {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if used+r.limits.ControlReserve > r.limits.MaxTotalState {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistence_unavailable")}
	}
	if err := r.ensureContextExecStore(); err != nil {
		return operation.ContextExecState{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	record := contextExecRecord{SchemaVersion: contextExecStoreSchemaVersion, State: want.Clone()}
	result := r.writer.Create(path, record)
	if result.Err == nil {
		return want.Clone(), true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		if err := readPrivateJSON(path, maxContextExecRecordBytes, &existing); err != nil {
			return operation.ContextExecState{}, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		return replayContextExecReserve(want, existing)
	}
	return operation.ContextExecState{}, false, result
}

func replayContextExecReserve(want operation.ContextExecState, existing contextExecRecord) (operation.ContextExecState, bool, app.StoreResult) {
	if err := existing.validate(); err != nil {
		return existing.State.Clone(), false, app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if !sameContextExecReservationIdentity(existing.State, want) {
		return existing.State.Clone(), false, app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(want.Request.ContextExecID)}
	}
	return existing.State.Clone(), false, app.StoreResult{Durability: app.DurableChange}
}

func sameContextExecReservationIdentity(a, b operation.ContextExecState) bool {
	return a.RequestFingerprint == b.RequestFingerprint && reflect.DeepEqual(a.Request, b.Request)
}

func (r *Repository) LookupContextExec(ctx context.Context, id string) (operation.ContextExecState, bool, error) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecState{}, false, err
	}
	if !validContextExecStoreID(id) {
		return operation.ContextExecState{}, false, fmt.Errorf("invalid context exec id")
	}
	var record contextExecRecord
	if _, err := os.Stat(r.legacyContextExecPath(id)); err == nil {
		return operation.ContextExecState{}, false, legacyContextExecError(id, "unknown")
	} else if !errors.Is(err, os.ErrNotExist) {
		return operation.ContextExecState{}, false, err
	}
	if err := readPrivateJSON(r.contextExecPath(id), maxContextExecRecordBytes, &record); errors.Is(err, ErrNotFound) {
		return operation.ContextExecState{}, false, nil
	} else if err != nil {
		return operation.ContextExecState{}, false, err
	}
	if err := record.validate(); err != nil || record.State.Request.ContextExecID != id {
		if err == nil {
			err = fmt.Errorf("context exec store identity mismatch")
		}
		return operation.ContextExecState{}, false, err
	}
	return record.State.Clone(), true, nil
}

func (r *Repository) AdvanceContextExec(ctx context.Context, id string, transition operation.ContextExecTransition) (operation.ContextExecState, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	unlock := r.lock(operation.ID(id))
	defer unlock()
	record, err := r.loadContextExecRecordUnlocked(id)
	if err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if transition.Lifecycle == contextexec.LifecycleHelperAuthenticated {
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: fmt.Errorf("helper authentication requires BindHelperGeneration")}
	}
	next, err := applyContextExecTransition(record.State, transition, r.now().UTC())
	if err != nil {
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	if reflect.DeepEqual(next, record.State) {
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange}
	}
	if next.Lifecycle == contextexec.LifecycleChildReserved {
		if err := r.validateContextExecChildReservation(ctx, next); err != nil {
			return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: err}
		}
	}
	record.State = next
	if err := record.validate(); err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Replace(r.contextExecPath(id), record)
	if result.Err != nil {
		return operation.ContextExecState{}, result
	}
	return next.Clone(), result
}

func applyContextExecTransition(current operation.ContextExecState, transition operation.ContextExecTransition, now time.Time) (operation.ContextExecState, error) {
	if err := transition.Lifecycle.Validate(); err != nil {
		return current, err
	}
	next := current.Clone()
	if transition.Helper != nil {
		helper := *transition.Helper
		next.Helper = &helper
	}
	if transition.ChildOperationID != "" || transition.ChildSessionID != "" {
		next.ChildOperationID, next.ChildSessionID = transition.ChildOperationID, transition.ChildSessionID
	}
	if transition.Result != nil {
		result := *transition.Result
		next.Result = &result
	}
	if current.Lifecycle == transition.Lifecycle {
		if current.Lifecycle == contextexec.LifecycleChildReserved {
			if transition.ExecutionAuthorized {
				next.ExecutionAuthorized = true
				if current.ExecutionAuthorized && reflect.DeepEqual(next, current) {
					return current, nil
				}
				if current.ExecutionAuthorized {
					return current, contextExecConflict(current.Request.ContextExecID)
				}
				next.UpdatedAt = now
				if err := next.Validate(); err != nil {
					return current, err
				}
				return next, nil
			}
			if current.ExecutionAuthorized {
				return current, contextExecConflict(current.Request.ContextExecID)
			}
		} else if transition.ExecutionAuthorized {
			return current, contextExecConflict(current.Request.ContextExecID)
		}
		next.UpdatedAt = current.UpdatedAt
		if reflect.DeepEqual(next, current) {
			return current, nil
		}
		return current, contextExecConflict(current.Request.ContextExecID)
	}
	if transition.ExecutionAuthorized {
		return current, contextExecConflict(current.Request.ContextExecID)
	}
	if transition.Lifecycle == contextexec.LifecycleCanonicalized && contextExecNoChildCanonicalTransition(current, next) {
		next.Lifecycle = transition.Lifecycle
		next.UpdatedAt = now
		if err := next.Validate(); err != nil {
			return current, err
		}
		return next, nil
	}
	if !current.Lifecycle.CanAdvanceTo(transition.Lifecycle) {
		return current, contextExecConflict(current.Request.ContextExecID)
	}
	if transition.Lifecycle == contextexec.LifecycleChildSpawned && !current.ExecutionAuthorized {
		return current, contextExecConflict(current.Request.ContextExecID)
	}
	next.Lifecycle = transition.Lifecycle
	next.UpdatedAt = now
	if err := next.Validate(); err != nil {
		return current, err
	}
	return next, nil
}

func contextExecNoChildCanonicalTransition(current, next operation.ContextExecState) bool {
	result := next.Result
	if result == nil || result.Lifecycle != contextexec.LifecycleCanonicalized || result.FailureCode == "" || result.Spawn.Succeeded || result.EvidenceAuthority != "" {
		return false
	}
	if result.Validate() != nil {
		return false
	}
	if result.Spawn.Attempted {
		return current.Lifecycle == contextexec.LifecycleChildReserved && current.ExecutionAuthorized && current.ChildOperationID != ""
	}
	return current.Lifecycle == contextexec.LifecycleHelperAuthenticated && !current.ExecutionAuthorized && current.ChildOperationID == ""
}

func (r *Repository) BindHelperGeneration(ctx context.Context, id string, helper contextexec.HelperBinding, finalContext contextexec.ContextBinding, boundaryObservedAt time.Time, verifierDigest string) (operation.ContextExecState, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := helper.Validate(); err != nil || finalContext.Validate() != nil || boundaryObservedAt.IsZero() || !validContextExecVerifier(verifierDigest) {
		if err == nil {
			err = fmt.Errorf("invalid context exec helper binding")
		}
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	unlock := r.lock(operation.ID(id))
	defer unlock()
	record, err := r.loadContextExecRecordUnlocked(id)
	if err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if record.State.Helper == nil || *record.State.Helper != helper || helper.RequestFingerprint != record.State.RequestFingerprint || !contextBindingMatchesExpectation(finalContext, record.State.Expectation) {
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(id)}
	}
	if record.ClaimVerifierDigest != "" {
		if record.ClaimVerifierDigest == verifierDigest && record.State.Lifecycle != contextexec.LifecycleHelperRequested && record.State.Context != nil && *record.State.Context == finalContext && record.State.BoundaryObservedAt.Equal(boundaryObservedAt) {
			return record.State.Clone(), app.StoreResult{Durability: app.DurableChange}
		}
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(id)}
	}
	if record.State.Lifecycle != contextexec.LifecycleHelperRequested || record.State.Context != nil || !record.State.BoundaryObservedAt.IsZero() {
		return record.State.Clone(), app.StoreResult{Durability: app.DurableChange, Err: contextExecConflict(id)}
	}
	next := record.State.Clone()
	contextCopy := finalContext
	next.Context = &contextCopy
	next.BoundaryObservedAt = boundaryObservedAt.UTC()
	next.Lifecycle = contextexec.LifecycleHelperAuthenticated
	next.UpdatedAt = r.now().UTC()
	if err := next.Validate(); err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	record.State, record.ClaimVerifierDigest = next, verifierDigest
	if err := record.validate(); err != nil {
		return operation.ContextExecState{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Replace(r.contextExecPath(id), record)
	if result.Err != nil {
		return operation.ContextExecState{}, result
	}
	return next.Clone(), result
}

func contextBindingMatchesExpectation(v contextexec.ContextBinding, e contextexec.ContextExpectation) bool {
	return v.SessionID == e.SessionID && v.AuthorityEpoch == e.AuthorityEpoch && v.ShellIdentity == e.ShellIdentity && v.BoundaryQuality == "shell_prompt" && v.CWDObserved == e.CWDObserved && v.PrivacyState == e.PrivacyState
}

func (r *Repository) validateContextExecChildReservation(ctx context.Context, state operation.ContextExecState) error {
	reservation, err := r.LoadOperation(ctx, state.ChildOperationID)
	if err != nil {
		return fmt.Errorf("context exec child reservation missing: %w", err)
	}
	if reservation.SchemaVersion != operation.ContextExecReservationSchemaVersion || reservation.SessionID != state.ChildSessionID || reservation.ContextExec == nil {
		return fmt.Errorf("invalid context exec child reservation")
	}
	binding := reservation.ContextExec
	if state.Context == nil || binding.ContextExecID != state.Request.ContextExecID || binding.ParentSessionID != operation.SessionID(state.Request.SessionID) || binding.AuthorityEpoch != state.Request.AuthorityEpoch || binding.RequestFingerprint != state.RequestFingerprint || reservation.RequestFingerprint != state.RequestFingerprint || reservation.CWD != state.Context.CWDObserved || reservation.TimeoutMS != state.Request.TimeoutMS || !reflect.DeepEqual(reservation.Argv, state.Request.Argv) {
		return contextExecConflict(state.Request.ContextExecID)
	}
	wantExecution, err := binding.ExecutionFingerprint(state.Context.CWDObserved, reservation.Executable)
	if err != nil || wantExecution != reservation.ExecutionFingerprint {
		return contextExecConflict(state.Request.ContextExecID)
	}
	return nil
}

func (r *Repository) loadContextExecRecordUnlocked(id string) (contextExecRecord, error) {
	if !validContextExecStoreID(id) {
		return contextExecRecord{}, fmt.Errorf("invalid context exec id")
	}
	var record contextExecRecord
	if err := readPrivateJSON(r.contextExecPath(id), maxContextExecRecordBytes, &record); err != nil {
		if errors.Is(err, ErrNotFound) {
			if _, legacyErr := os.Stat(r.legacyContextExecPath(id)); legacyErr == nil {
				return record, legacyContextExecError(id, "unknown")
			} else if !errors.Is(legacyErr, os.ErrNotExist) {
				return record, legacyErr
			}
		}
		return record, err
	}
	if err := record.validate(); err != nil || record.State.Request.ContextExecID != id {
		if err == nil {
			err = fmt.Errorf("context exec store identity mismatch")
		}
		return record, err
	}
	return record, nil
}

func (r *Repository) contextExecRoot() string      { return filepath.Join(r.root, "context-exec") }
func (r *Repository) contextExecDir() string       { return filepath.Join(r.contextExecRoot(), "v2") }
func (r *Repository) legacyContextExecDir() string { return filepath.Join(r.contextExecRoot(), "v1") }

func (r *Repository) ensureContextExecStore() error {
	if err := ensurePrivateDir(r.contextExecRoot()); err != nil {
		return fmt.Errorf("context exec store root: %w", err)
	}
	if err := ensurePrivateDir(r.contextExecDir()); err != nil {
		return fmt.Errorf("context exec store version: %w", err)
	}
	return nil
}
func (r *Repository) contextExecPath(id string) string {
	return filepath.Join(r.contextExecDir(), id+".json")
}
func (r *Repository) legacyContextExecPath(id string) string {
	return filepath.Join(r.legacyContextExecDir(), id+".json")
}

func validContextExecStoreID(value string) bool {
	if value == "" || len(value) > contextexec.MaxContextExecIDBytes || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for i := range value {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validContextExecVerifier(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func legacyContextExecError(id, sessionID string) error {
	return failure.New(failure.ContextExecAmbiguous, map[string]string{"context_exec_id": id, "session_id": sessionID, "reason": "legacy_v1_record"}, nil)
}

func contextExecConflict(id string) error {
	return failure.New(failure.OperationConflict, map[string]string{"operation_id": id}, nil)
}
