package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func delegatedFingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Service) writeDelegated(ctx context.Context, live *liveSession, req WriteRequest) (View, error) {
	if req.EOF {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"field": "eof"}, fmt.Errorf("delegated interactive EOF is not a byte write"))
	}
	if req.Chars == "" {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"field": "chars"}, fmt.Errorf("delegated write requires chars"))
	}
	id := delegated.MutationIdentity{SessionID: req.SessionID, Epoch: req.AuthorityEpoch, Kind: delegated.MutationWrite, Offset: req.InputOffset, Fingerprint: delegatedFingerprint("write", req.Chars)}

	live.delegatedMutationMu.Lock()
	defer live.delegatedMutationMu.Unlock()
	record, replay, err := s.admitDelegatedMutation(ctx, live, id, func() error {
		live.mu.Lock()
		defer live.mu.Unlock()
		if live.state != session.Running {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_writable"}, fmt.Errorf("session_not_writable"))
		}
		if req.InputOffset != live.accepted {
			return failure.New(failure.OperationConflict, nil, fmt.Errorf("input offset %d does not match next offset %d", req.InputOffset, live.accepted))
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if replay {
		return s.replayDelegatedWrite(live, req, record)
	}

	if err := s.options.DelegatedRuntime.Write(ctx, live.delegatedRef, []byte(req.Chars)); err != nil {
		_, _ = s.delegatedStore().CompleteDelegatedMutation(context.Background(), id, delegated.MutationOutcomeUnknown, "provider_ambiguous")
		return View{}, failure.Normalize(err)
	}
	completed, result := s.delegatedStore().CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "succeeded")
	if result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	live.mu.Lock()
	live.accepted = req.InputOffset + int64(len([]byte(req.Chars)))
	live.delivered = live.accepted
	live.notify()
	state := live.state
	live.mu.Unlock()
	_ = completed
	return View{SessionID: req.SessionID, State: state, AuthorityEpoch: req.AuthorityEpoch, AcceptedInputBytes: len([]byte(req.Chars)), NextInputOffset: req.InputOffset + int64(len([]byte(req.Chars)))}, nil
}

func (s *Service) replayDelegatedWrite(live *liveSession, req WriteRequest, record delegated.MutationRecord) (View, error) {
	if record.State != delegated.MutationCompleted || record.Outcome != "succeeded" {
		if err := s.reconcileUnknownDelegatedMutation(context.Background(), live, record); err != nil {
			return View{}, err
		}
		return View{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.SessionID, "reason": "mutation_outcome_unknown", "current_epoch": fmt.Sprint(record.Identity.Epoch)}, nil)
	}
	live.mu.Lock()
	state := live.state
	live.mu.Unlock()
	return View{SessionID: req.SessionID, State: state, AuthorityEpoch: record.Identity.Epoch, AcceptedInputBytes: len([]byte(req.Chars)), NextInputOffset: req.InputOffset + int64(len([]byte(req.Chars)))}, nil
}

func (s *Service) killDelegated(ctx context.Context, live *liveSession, req KillRequest) (View, error) {
	if req.KillID == "" {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"field": "kill_id"}, fmt.Errorf("delegated kill id required"))
	}
	switch req.Signal {
	case "INT", "TERM", "KILL":
	default:
		return View{}, failure.New(failure.InvalidInput, map[string]string{"field": "signal"}, fmt.Errorf("unsupported delegated signal"))
	}
	id := delegated.MutationIdentity{SessionID: req.SessionID, Epoch: req.AuthorityEpoch, Kind: delegated.MutationKill, IdempotencyID: req.KillID, Offset: -1, Fingerprint: delegatedFingerprint("kill", req.Signal)}

	live.delegatedMutationMu.Lock()
	defer live.delegatedMutationMu.Unlock()
	record, replay, err := s.admitDelegatedMutation(ctx, live, id, func() error {
		live.mu.Lock()
		defer live.mu.Unlock()
		if live.state != session.Running {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "session_not_live"}, fmt.Errorf("session_not_live"))
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if replay {
		return s.replayDelegatedKill(live, req, record)
	}
	if err := s.options.DelegatedRuntime.Signal(ctx, live.delegatedRef, req.Signal); err != nil {
		_, _ = s.delegatedStore().CompleteDelegatedMutation(context.Background(), id, delegated.MutationOutcomeUnknown, "provider_ambiguous")
		return View{}, failure.Normalize(err)
	}
	if _, result := s.delegatedStore().CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "succeeded"); result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	live.mu.Lock()
	live.terminalTarget = session.Killed
	live.signal = receipt.SignalEvidence{Requested: req.Signal, Attempted: true, Succeeded: true}
	live.notify()
	state := live.state
	live.mu.Unlock()
	return View{SessionID: req.SessionID, State: state, AuthorityEpoch: req.AuthorityEpoch, KillID: req.KillID, Signal: req.Signal, SignalAttempt: receipt.SignalEvidence{Requested: req.Signal, Attempted: true, Succeeded: true}}, nil
}

func (s *Service) replayDelegatedKill(live *liveSession, req KillRequest, record delegated.MutationRecord) (View, error) {
	if record.State != delegated.MutationCompleted || record.Outcome != "succeeded" {
		if err := s.reconcileUnknownDelegatedMutation(context.Background(), live, record); err != nil {
			return View{}, err
		}
		return View{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.SessionID, "reason": "mutation_outcome_unknown", "current_epoch": fmt.Sprint(record.Identity.Epoch)}, nil)
	}
	live.mu.Lock()
	state := live.state
	live.mu.Unlock()
	evidence := receipt.SignalEvidence{Requested: req.Signal, Attempted: true, Succeeded: true}
	return View{SessionID: req.SessionID, State: state, AuthorityEpoch: record.Identity.Epoch, KillID: req.KillID, Signal: req.Signal, SignalAttempt: evidence}, nil
}

func (s *Service) admitDelegatedMutation(ctx context.Context, live *liveSession, id delegated.MutationIdentity, preReserve func() error) (delegated.MutationRecord, bool, error) {
	store := s.delegatedStore()
	if store == nil {
		return delegated.MutationRecord{}, false, failure.New(failure.PersistenceUnavailable, nil, fmt.Errorf("delegated store unavailable"))
	}
	known, found, err := store.LookupDelegatedMutation(ctx, id)
	if err != nil {
		return delegated.MutationRecord{}, false, failure.Normalize(err)
	}
	if found {
		decision, decideErr := delegated.DecideMutation(&known, id, delegated.MutationContext{})
		if decideErr != nil {
			return delegated.MutationRecord{}, false, decideErr
		}
		return decision.Record, true, nil
	}
	if preReserve != nil {
		if err := preReserve(); err != nil {
			return delegated.MutationRecord{}, false, err
		}
	}
	binding, err := store.LoadDelegatedBinding(ctx, operation.SessionID(id.SessionID))
	if err != nil {
		return delegated.MutationRecord{}, false, failure.Normalize(err)
	}
	if binding.Lifecycle != delegated.LifecycleLive {
		return delegated.MutationRecord{}, false, failure.New(failure.SessionControlNotOwned, map[string]string{"session_id": id.SessionID, "owner": string(delegated.OwnerNone), "required_owner": string(delegated.OwnerAgent), "current_epoch": fmt.Sprint(binding.AuthorityEpoch)}, nil)
	}
	obs, err := s.options.DelegatedRuntime.Inspect(ctx, live.delegatedRef)
	if err != nil {
		return delegated.MutationRecord{}, false, err
	}
	if obs.Provider != binding.ProviderIdentity() {
		return delegated.MutationRecord{}, false, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": id.SessionID, "provider_id": obs.Provider.ID, "provider_version": fmt.Sprint(obs.Provider.Version), "expected_provider_id": binding.ProviderID, "expected_provider_version": fmt.Sprint(binding.ProviderVersion)}, nil)
	}
	authority := delegated.ReconcileAuthority(delegated.ReconcileInput{Epoch: binding.AuthorityEpoch, DesiredOwner: binding.DesiredOwner, ObservedOwner: obs.Owner, ProviderIdentity: obs.Provider, ProviderCurrent: obs.ProviderCurrent})
	decision, err := delegated.DecideMutation(nil, id, delegated.MutationContext{CurrentEpoch: binding.AuthorityEpoch, CurrentOwner: authority.Owner})
	if err != nil {
		return delegated.MutationRecord{}, false, err
	}
	reserved, created, result := store.ReserveDelegatedMutation(ctx, decision.Record.Identity)
	if result.Err != nil {
		return delegated.MutationRecord{}, false, failure.Normalize(result.Err)
	}
	if !created {
		return reserved, true, nil
	}
	return reserved, false, nil
}

func (s *Service) reconcileUnknownDelegatedMutation(ctx context.Context, live *liveSession, record delegated.MutationRecord) error {
	obs, err := s.options.DelegatedRuntime.Inspect(ctx, live.delegatedRef)
	if err != nil {
		return err
	}
	binding, err := s.delegatedStore().LoadDelegatedBinding(ctx, operation.SessionID(record.Identity.SessionID))
	if err != nil {
		return failure.Normalize(err)
	}
	if obs.Provider != binding.ProviderIdentity() || !obs.ProviderCurrent {
		return failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": record.Identity.SessionID, "reason": "mutation_provider_unproven", "current_epoch": fmt.Sprint(binding.AuthorityEpoch)}, nil)
	}
	return nil
}
