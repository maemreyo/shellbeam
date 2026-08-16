package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) preparePersistentLifecycleObservation(ctx context.Context, from, to persistent.Binding) (observation.ChangeSeq, app.StoreResult) {
	var kind observation.EventKind
	var transition, summary string
	switch {
	case from.Lifecycle == persistent.LifecycleProvisioning && to.Lifecycle == persistent.LifecycleLive:
		kind, transition, summary = observation.EventPersistentSessionStarted, "started", "persistent session started"
	case to.Lifecycle == persistent.LifecycleTerminal:
		kind, transition, summary = observation.EventPersistentSessionTerminal, "terminal", "persistent session terminal"
	case to.Lifecycle == persistent.LifecycleLost:
		kind, transition, summary = observation.EventPersistentSessionLost, "lost", "persistent session lost"
	default:
		return 0, app.StoreResult{Durability: app.DurableChange}
	}
	request := observation.PrepareRequest{Kind: kind, Correlation: persistentBindingCorrelation(to), SubjectRef: "persistent:" + to.SessionID + ":" + transition, Summary: summary}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) AdvancePersistentReattachedSession(ctx context.Context, want session.Snapshot) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	id, err := operation.ParseSessionID(want.SessionID)
	if err != nil || want.SchemaVersion != 1 || want.DaemonIncarnation == "" || want.State != session.Running || !want.OutputAvailable {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("invalid persistent reattach snapshot")}
	}
	current, err := r.LoadSession(ctx, id)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if current.SchemaVersion != 1 || current.SessionID != want.SessionID || current.OperationID != want.OperationID || current.State.Terminal() {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistent reattach session mismatch")}
	}
	reservation, err := r.LoadOperation(ctx, operation.ID(current.OperationID))
	if err != nil || !reservation.Persistent || string(reservation.SessionID) != want.SessionID || string(reservation.OperationID) != want.OperationID {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistent reattach reservation mismatch")}
	}
	binding, err := r.LoadPersistentBinding(ctx, id)
	if err != nil || binding.Lifecycle != persistent.LifecycleLive || binding.OperationID != want.OperationID {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("persistent reattach binding mismatch")}
	}
	if current.DaemonIncarnation == want.DaemonIncarnation {
		return r.AdvanceSession(ctx, want)
	}
	request := observation.PrepareRequest{Kind: observation.EventPersistentSessionReattached, Correlation: correlationFromReservation(reservation), SubjectRef: "persistent:" + want.SessionID + ":reattached:" + want.DaemonIncarnation, Summary: "persistent session reattached"}
	seq, prepared := r.prepareExecutionObservation(ctx, request)
	if prepared.Err != nil {
		return prepared
	}
	result := r.AdvanceSession(ctx, want)
	r.finishPersistentObservation(seq, result, func() bool {
		got, loadErr := r.LoadSession(context.Background(), id)
		return loadErr == nil && got.DaemonIncarnation == want.DaemonIncarnation
	})
	return withObservationSeq(result, seq)
}

func (r *Repository) finishPersistentObservation(seq observation.ChangeSeq, result app.StoreResult, canonicalMatches func() bool) {
	if seq == 0 {
		return
	}
	if result.Err == nil || result.Durability == app.DurableChange {
		r.commitObservationBestEffort(seq)
		return
	}
	if result.Durability == app.NoDurableChange {
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		return
	}
	if canonicalMatches != nil && canonicalMatches() {
		r.commitObservationBestEffort(seq)
	}
}

func persistentBindingCorrelation(binding persistent.Binding) observation.Correlation {
	return observation.Correlation{OperationID: binding.OperationID, SessionID: binding.SessionID, ActivityID: binding.ActivityID, WorkspaceID: binding.WorkspaceID}
}

func (r *Repository) persistentObservationSubjectPresent(ctx context.Context, obligation observation.ObservationObligation) (bool, error) {
	parts := strings.Split(obligation.SubjectRef, ":")
	if len(parts) < 3 || parts[0] != "persistent" {
		return false, fmt.Errorf("invalid persistent observation subject")
	}
	id, err := operation.ParseSessionID(parts[1])
	if err != nil {
		return false, fmt.Errorf("invalid persistent observation session")
	}
	switch obligation.Kind {
	case observation.EventPersistentSessionReattached:
		if len(parts) != 4 || parts[2] != "reattached" || parts[3] == "" {
			return false, fmt.Errorf("invalid persistent reattach subject")
		}
		snap, loadErr := r.LoadSession(ctx, id)
		if errors.Is(loadErr, ErrNotFound) {
			return false, nil
		}
		return loadErr == nil && snap.DaemonIncarnation == parts[3], loadErr
	case observation.EventPersistentSessionStarted, observation.EventPersistentSessionTerminal, observation.EventPersistentSessionLost:
		if len(parts) != 3 {
			return false, fmt.Errorf("invalid persistent lifecycle subject")
		}
		binding, loadErr := r.LoadPersistentBinding(ctx, id)
		if errors.Is(loadErr, ErrNotFound) {
			return false, nil
		}
		if loadErr != nil {
			return false, loadErr
		}
		switch obligation.Kind {
		case observation.EventPersistentSessionTerminal:
			return parts[2] == "terminal" && binding.Lifecycle == persistent.LifecycleTerminal, nil
		case observation.EventPersistentSessionLost:
			return parts[2] == "lost" && binding.Lifecycle == persistent.LifecycleLost, nil
		case observation.EventPersistentSessionStarted:
			if parts[2] != "started" {
				return false, fmt.Errorf("invalid persistent started subject")
			}
			if binding.Lifecycle == persistent.LifecycleLive {
				return true, nil
			}
			rec, recErr := r.LoadReceipt(ctx, id)
			if recErr == nil && rec.Spawn.Succeeded {
				return true, nil
			}
			if recErr != nil && !errors.Is(recErr, ErrNotFound) {
				return false, recErr
			}
			return false, nil
		}
	}
	return false, nil
}
