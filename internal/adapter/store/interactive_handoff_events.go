package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type handoffObservationSpec struct {
	kind       observation.EventKind
	transition string
	summary    string
	epoch      delegated.AuthorityEpoch
}

func handoffCreateObservationSpec(state handoff.State) handoffObservationSpec {
	return handoffObservationSpec{kind: observation.EventHandoffRequested, transition: "requested", summary: "interactive handoff requested", epoch: state.AuthorityEpoch}
}

func handoffAdvanceObservationSpecs(from, to handoff.State) []handoffObservationSpec {
	out := make([]handoffObservationSpec, 0, 2)
	add := func(kind observation.EventKind, transition, summary string) {
		out = append(out, handoffObservationSpec{kind: kind, transition: transition, summary: summary, epoch: to.AuthorityEpoch})
	}
	if from.HumanClient == nil && to.HumanClient != nil {
		add(observation.EventHandoffAttached, "attached", "interactive handoff human client attached")
	}
	if from.Phase != handoff.PhaseHumanOwned && to.Phase == handoff.PhaseHumanOwned {
		add(observation.EventHandoffHumanOwned, "human-owned", "interactive handoff human authority active")
	}
	if from.Phase != handoff.PhaseHumanFencing && to.Phase == handoff.PhaseHumanFencing {
		add(observation.EventHandoffReclaimStarted, "reclaim-started", "interactive handoff reclaim started")
	}
	if from.Phase != handoff.PhaseAgentOwned && to.Phase == handoff.PhaseAgentOwned {
		add(observation.EventHandoffReclaimed, "reclaimed", "interactive handoff reclaimed by agent")
	}
	if from.FailureCode != failure.HandoffClientLost && to.FailureCode == failure.HandoffClientLost {
		add(observation.EventHandoffClientLost, "client-lost", "interactive handoff human client lost")
	}
	if from.FailureCode != failure.HandoffExpired && to.FailureCode == failure.HandoffExpired {
		add(observation.EventHandoffExpired, "expired", "interactive handoff expired")
	}
	if to.FailureCode != failure.HandoffExpired && from.Phase != handoff.PhaseAborted && to.Phase == handoff.PhaseAborted {
		add(observation.EventHandoffAborted, "aborted", "interactive handoff aborted")
	}
	return out
}

func (r *Repository) prepareHandoffObservations(ctx context.Context, state handoff.State, specs []handoffObservationSpec) ([]observation.ChangeSeq, app.StoreResult) {
	if len(specs) == 0 {
		return nil, app.StoreResult{Durability: app.DurableChange}
	}
	seqs := make([]observation.ChangeSeq, 0, len(specs))
	for _, spec := range specs {
		seq, result := r.prepareExecutionObservation(ctx, observation.PrepareRequest{
			Kind:        spec.kind,
			Correlation: observation.Correlation{SessionID: state.SessionID},
			SubjectRef:  handoffObservationSubject(state.HandoffID, spec.epoch, spec.transition),
			Summary:     spec.summary,
		})
		if result.Err != nil {
			for _, prepared := range seqs {
				r.abortObservationBestEffort(prepared, observationAbortWriteFailed)
			}
			return nil, result
		}
		seqs = append(seqs, seq)
	}
	return seqs, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) finishHandoffObservations(seqs []observation.ChangeSeq, result app.StoreResult, canonicalMatches func() bool) app.StoreResult {
	if len(seqs) == 0 {
		return result
	}
	commit := result.Err == nil
	if !commit && result.Durability != app.NoDurableChange && canonicalMatches != nil {
		commit = canonicalMatches()
	}
	for _, seq := range seqs {
		if commit {
			r.commitObservationBestEffort(seq)
		} else if result.Durability == app.NoDurableChange {
			r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		}
	}
	return withObservationSeq(result, seqs[len(seqs)-1])
}

func handoffObservationSubject(id string, epoch delegated.AuthorityEpoch, transition string) string {
	return fmt.Sprintf("handoff:%s:%d:%s", id, epoch, transition)
}

func parseHandoffObservationSubject(subject string) (string, delegated.AuthorityEpoch, string, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 4 || parts[0] != "handoff" || !validHandoffStoreID(parts[1]) {
		return "", 0, "", fmt.Errorf("invalid handoff observation subject")
	}
	raw, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid handoff observation epoch")
	}
	epoch := delegated.AuthorityEpoch(raw)
	if err := epoch.Validate(); err != nil {
		return "", 0, "", fmt.Errorf("invalid handoff observation epoch")
	}
	return parts[1], epoch, parts[3], nil
}

func (r *Repository) handoffObservationSubjectPresent(ctx context.Context, obligation observation.ObservationObligation) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	id, epoch, transition, err := parseHandoffObservationSubject(obligation.SubjectRef)
	if err != nil {
		return false, err
	}
	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	if record, loadErr := r.loadHandoffRecordLocked(id); loadErr == nil {
		if handoffStateProvesObservation(record.State, obligation.Kind, epoch, transition) {
			return true, nil
		}
	} else if !errors.Is(loadErr, ErrNotFound) {
		return false, loadErr
	}
	tx, txErr := r.loadHandoffTransactionLocked(id)
	if errors.Is(txErr, ErrNotFound) {
		return false, nil
	}
	if txErr != nil {
		return false, txErr
	}
	binding, bindErr := r.loadDelegatedBindingLocked(operation.SessionID(tx.TargetBinding.SessionID))
	if bindErr != nil {
		return false, bindErr
	}
	allowPrior := obligation.Kind == observation.EventHandoffRequested
	if binding != tx.TargetBinding && (!allowPrior || binding != tx.PriorBinding) {
		return false, nil
	}
	return handoffStateProvesObservation(tx.Record.State, obligation.Kind, epoch, transition), nil
}

func handoffStateProvesObservation(state handoff.State, kind observation.EventKind, epoch delegated.AuthorityEpoch, transition string) bool {
	if state.AuthorityEpoch != epoch {
		return false
	}
	switch kind {
	case observation.EventHandoffRequested:
		return transition == "requested" && state.Phase == handoff.PhaseAgentFencing
	case observation.EventHandoffAttached:
		return transition == "attached" && state.HumanClient != nil
	case observation.EventHandoffHumanOwned:
		return transition == "human-owned" && state.Phase == handoff.PhaseHumanOwned
	case observation.EventHandoffReclaimStarted:
		return transition == "reclaim-started" && state.Phase == handoff.PhaseHumanFencing
	case observation.EventHandoffReclaimed:
		return transition == "reclaimed" && state.Phase == handoff.PhaseAgentOwned && state.DesiredOwner == delegated.OwnerAgent
	case observation.EventHandoffAborted:
		return transition == "aborted" && state.Phase == handoff.PhaseAborted && state.FailureCode != failure.HandoffExpired
	case observation.EventHandoffClientLost:
		return transition == "client-lost" && state.FailureCode == failure.HandoffClientLost
	case observation.EventHandoffExpired:
		return transition == "expired" && state.FailureCode == failure.HandoffExpired
	default:
		return false
	}
}

func (r *Repository) handoffObservationTransactionMatchesLocked(tx handoffTransaction, allowPrior bool) bool {
	current, err := r.loadDelegatedBindingLocked(operation.SessionID(tx.TargetBinding.SessionID))
	if err != nil {
		return false
	}
	if current != tx.TargetBinding && (!allowPrior || current != tx.PriorBinding) {
		return false
	}
	if record, err := r.loadHandoffRecordLocked(tx.Record.Request.HandoffID); err == nil {
		return record.State == tx.Record.State
	}
	_, err = r.loadHandoffTransactionLocked(tx.Record.Request.HandoffID)
	return err == nil
}
