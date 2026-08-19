package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestDelegatedMutationLedgerExactReplayConflictAndTransitions(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-mutation", "session-mutation", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_mutation")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}

	id := delegated.MutationIdentity{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 1, Fingerprint: "write_fp_01"}
	rec, created, got := r.ReserveDelegatedMutation(context.Background(), id)
	if got.Err != nil || !created || rec.State != delegated.MutationReserved {
		t.Fatalf("reserve rec=%#v created=%v result=%#v", rec, created, got)
	}
	replay, created, got := r.ReserveDelegatedMutation(context.Background(), id)
	if got.Err != nil || created || replay != rec {
		t.Fatalf("replay rec=%#v created=%v result=%#v", replay, created, got)
	}

	conflict := id
	conflict.Fingerprint = "write_fp_02"
	if _, _, got := r.ReserveDelegatedMutation(context.Background(), conflict); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("fingerprint conflict=%v", got.Err)
	}

	delivered, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationDelivered, "provider_ack")
	if got.Err != nil || delivered.State != delegated.MutationDelivered {
		t.Fatalf("delivered=%#v result=%#v", delivered, got)
	}
	completed, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "accepted")
	if got.Err != nil || completed.State != delegated.MutationCompleted || completed.Outcome != "accepted" {
		t.Fatalf("completed=%#v result=%#v", completed, got)
	}
	if _, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationDelivered, "late"); got.Err == nil {
		t.Fatal("terminal mutation regressed")
	}

	found, ok, err := r.LookupDelegatedMutation(context.Background(), id)
	if err != nil || !ok || found != completed {
		t.Fatalf("lookup=%#v ok=%v err=%v", found, ok, err)
	}
}

func TestDelegatedMutationLedgerIsBoundedWithoutEvictingReplayAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 2)
	res := task4DelegatedReservation("op-limit", "session-limit", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_limit")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}
	ids := []delegated.MutationIdentity{
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 1, Fingerprint: "fp0"},
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 1, NextOffset: 2, Fingerprint: "fp1"},
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 2, NextOffset: 3, Fingerprint: "fp2"},
	}
	for _, id := range ids[:2] {
		if _, _, got := r.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
			t.Fatal(got.Err)
		}
		if _, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "accepted"); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	if _, _, got := r.ReserveDelegatedMutation(context.Background(), ids[2]); !errors.Is(got.Err, failure.CapacityExceeded) {
		t.Fatalf("overflow=%v", got.Err)
	}

	r = delegatedRepository(t, root, 2)
	for _, id := range ids[:2] {
		if rec, ok, err := r.LookupDelegatedMutation(context.Background(), id); err != nil || !ok || rec.State != delegated.MutationCompleted {
			t.Fatalf("restart replay id=%#v rec=%#v ok=%v err=%v", id, rec, ok, err)
		}
	}
}

func TestDelegatedRecoveryStateReconstructsContiguousCompletedWritePrefix(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-recovery-offset", "session-recovery-offset", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_recovery_offset")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}
	writes := []delegated.MutationIdentity{
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 3, Fingerprint: "fp-recovery-0"},
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 3, NextOffset: 5, Fingerprint: "fp-recovery-3"},
	}
	for _, id := range writes {
		if _, _, got := r.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
			t.Fatal(got.Err)
		}
		if _, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "succeeded"); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	control := delegated.MutationIdentity{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationKill, IdempotencyID: "old-kill", Offset: -1, NextOffset: -1, Fingerprint: "fp-old-kill"}
	if _, _, got := r.ReserveDelegatedMutation(context.Background(), control); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.CompleteDelegatedMutation(context.Background(), control, delegated.MutationFailed, "rejected"); got.Err != nil {
		t.Fatal(got.Err)
	}
	state, err := r.LoadDelegatedRecoveryState(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || state.NextInputOffset != 5 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestDelegatedRecoveryStateFencesUnresolvedGapAndCorruptLedger(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *Repository, delegated.Binding){
		"unresolved": func(t *testing.T, r *Repository, b delegated.Binding) {
			id := delegated.MutationIdentity{SessionID: b.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 2, Fingerprint: "fp-unresolved"}
			if _, _, got := r.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
				t.Fatal(got.Err)
			}
		},
		"gap": func(t *testing.T, r *Repository, b delegated.Binding) {
			for _, id := range []delegated.MutationIdentity{
				{SessionID: b.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 2, Fingerprint: "fp-gap-0"},
				{SessionID: b.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 3, NextOffset: 4, Fingerprint: "fp-gap-3"},
			} {
				if _, _, got := r.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
					t.Fatal(got.Err)
				}
				if _, got := r.CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "succeeded"); got.Err != nil {
					t.Fatal(got.Err)
				}
			}
		},
		"corrupt": func(t *testing.T, r *Repository, b delegated.Binding) {
			dir := r.delegatedSessionMutationDir(operation.SessionID(b.SessionID))
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not-json"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
			res := task4DelegatedReservation("op-recovery-"+name, "session-recovery-"+name, "")
			reserveDelegatedOperation(t, r, res)
			binding, ref := delegatedBindingAndRef(res, "provider_ref_recovery_"+name)
			if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
				t.Fatal(got.Err)
			}
			mutate(t, r, binding)
			if _, err := r.LoadDelegatedRecoveryState(context.Background(), operation.SessionID(binding.SessionID)); !errors.Is(err, failure.DelegatedReconcileBlocked) {
				t.Fatalf("err=%v want delegated_reconcile_blocked", err)
			}
		})
	}
}

func TestDelegatedOutputBytesUsesRetainedExtent(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-recovery-output", "session-recovery-output", "")
	reserveDelegatedOperation(t, r, res)
	for _, chunk := range [][]byte{[]byte("abc"), []byte("defgh")} {
		if _, got := r.AppendOutput(context.Background(), res.SessionID, chunk); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	got, err := r.DelegatedOutputBytes(context.Background(), res.SessionID)
	if err != nil || got != 8 {
		t.Fatalf("bytes=%d err=%v", got, err)
	}
}
