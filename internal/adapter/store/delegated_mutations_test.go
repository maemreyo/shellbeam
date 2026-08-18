package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestDelegatedMutationLedgerExactReplayConflictAndTransitions(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-mutation", "session-mutation", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_mutation")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}

	id := delegated.MutationIdentity{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, Fingerprint: "write_fp_01"}
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
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, Fingerprint: "fp0"},
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 1, Fingerprint: "fp1"},
		{SessionID: binding.SessionID, Epoch: 1, Kind: delegated.MutationWrite, Offset: 2, Fingerprint: "fp2"},
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
