package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) prepareMutationScopeObservation(ctx context.Context, scopeID, activityID, workspaceID, summary string) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind:        observation.EventMutationScopeChanged,
		Correlation: observation.Correlation{ActivityID: activityID, WorkspaceID: workspaceID},
		SubjectRef:  scopeID, Summary: summary,
	}
	return r.prepareExecutionObservation(ctx, request)
}

func mutationScopeProof(pending mutationScopePending, identity core.ScopeIdentity) (mutationScopeObservationProof, error) {
	proof := mutationScopeObservationProof{
		SchemaVersion: mutationScopeStoreSchema, ChangeSeq: pending.ObservationSeq,
		MutationID: pending.Receipt.MutationID, ScopeID: pending.ScopeID,
		ActivityID: identity.ActivityID, WorkspaceID: string(identity.WorkspaceID), Result: pending.Receipt.Result,
	}
	return proof, proof.validate()
}

func (r *Repository) ensureMutationScopeObservationProofLocked(pending mutationScopePending) app.StoreResult {
	if pending.ObservationSeq == 0 {
		return app.StoreResult{Durability: app.DurableChange}
	}
	var identity core.ScopeIdentity
	if pending.Kind == "set" && pending.Identity != nil {
		identity = *pending.Identity
	} else {
		loaded, found, err := r.loadMutationScopeIdentityUnlocked(pending.ScopeID)
		if err != nil {
			return app.StoreResult{Durability: app.NoDurableChange, Err: err}
		}
		if !found {
			return app.StoreResult{Durability: app.AmbiguousChange, Err: fmt.Errorf("mutation scope observation identity missing")}
		}
		identity = loaded
	}
	proof, err := mutationScopeProof(pending, identity)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.mutationScopeObservationProofPath(uint64(proof.ChangeSeq))
	result := r.writer.Create(path, proof)
	if result.Err == nil {
		return result
	}
	if errors.Is(result.Err, os.ErrExist) {
		var existing mutationScopeObservationProof
		if readErr := readMutationScopeJSON(path, &existing); readErr == nil && reflect.DeepEqual(existing, proof) {
			return app.StoreResult{Durability: app.DurableChange}
		}
	}
	return result
}

func (r *Repository) mutationScopeObservationSubjectPresent(obligation observation.ObservationObligation) (bool, error) {
	r.mutationScopeMu.Lock()
	defer r.mutationScopeMu.Unlock()
	var proof mutationScopeObservationProof
	path := r.mutationScopeObservationProofPath(uint64(obligation.ChangeSeq))
	if err := readMutationScopeJSON(path, &proof); errors.Is(err, ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := proof.validate(); err != nil {
		return false, err
	}
	if proof.ChangeSeq != obligation.ChangeSeq || proof.ScopeID != obligation.SubjectRef || proof.ActivityID != obligation.Correlation.ActivityID || proof.WorkspaceID != obligation.Correlation.WorkspaceID {
		return false, fmt.Errorf("mutation scope observation proof mismatch")
	}
	wantSummary := "set"
	if proof.Result == core.ResultReleased {
		wantSummary = "released"
	}
	if obligation.Summary != wantSummary {
		return false, fmt.Errorf("mutation scope observation summary mismatch")
	}
	claim, found, err := r.loadMutationScopeClaimUnlocked(proof.MutationID)
	if err != nil || !found {
		return false, err
	}
	return claim.Status == "committed" && claim.Receipt != nil && claim.Receipt.Result == proof.Result && claim.ScopeID == proof.ScopeID, nil
}

func (r *Repository) finishMutationScopeObservation(seq observation.ChangeSeq) {
	if seq == 0 {
		return
	}
	result := r.CommitObservationSequence(context.Background(), uint64(seq))
	if result.Err == nil {
		r.removeMutationScopeObservationProofBestEffort(seq)
	}
}

func (r *Repository) finishFailedMutationScopePendingObservation(pending mutationScopePending, result app.StoreResult) {
	if pending.ObservationSeq == 0 || result.Err == nil {
		return
	}
	if result.Durability == app.NoDurableChange {
		r.abortObservationBestEffort(pending.ObservationSeq, observationAbortWriteFailed)
		return
	}
	current, found, err := r.loadMutationScopePendingLocked()
	if err == nil && (!found || !samePending(current, pending)) {
		r.abortObservationBestEffort(pending.ObservationSeq, observationAbortWriteFailed)
	}
}

func (r *Repository) removeMutationScopeObservationProofBestEffort(seq observation.ChangeSeq) {
	path := r.mutationScopeObservationProofPath(uint64(seq))
	if err := os.Remove(path); err == nil {
		_ = r.writer.syncParent("mutation_scope_proof_remove", filepath.Dir(path)).Err
	}
}

func (r *Repository) cleanupMutationScopeObservationProofs() error {
	dir := filepath.Join(r.root, "mutation-scopes", "observation-proofs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > MaxObservationListRecords {
		return fmt.Errorf("mutation scope observation proof limit exceeded")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unsafe mutation scope observation proof entry")
		}
		var proof mutationScopeObservationProof
		if err := readMutationScopeJSON(filepath.Join(dir, entry.Name()), &proof); err != nil {
			return err
		}
		obligation, err := r.readObservation(proof.ChangeSeq)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if obligation.State != observation.ObligationPrepared {
			r.removeMutationScopeObservationProofBestEffort(proof.ChangeSeq)
		}
	}
	return nil
}

func (p mutationScopeObservationProof) validateCorrelation() error {
	if _, err := activity.ParseID(p.ActivityID); err != nil {
		return err
	}
	_, err := workspace.ParseWorkspaceID(p.WorkspaceID)
	return err
}
