package store

import (
	"context"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func reserveHandoffAt(t *testing.T, r *Repository, suffix string, now time.Time) (handoff.Request, handoff.State) {
	t.Helper()
	r.now = func() time.Time { return now }
	res := task4DelegatedReservation("op-expiry-"+suffix, "session-expiry-"+suffix, "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_expiry_"+suffix)
	if _, _, result := r.ReserveDelegatedBinding(context.Background(), binding, ref); result.Err != nil {
		t.Fatal(result.Err)
	}
	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = now.Add(time.Nanosecond)
	if result := r.AdvanceDelegatedBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	req, initial := h2HandoffRequestAndState(live, "expiry-"+suffix)
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	return req, initial
}

func TestListExpiredHandoffsUsesCreatedAtBoundAndSkipsTerminalHandoffs(t *testing.T) {
	r := delegatedRepository(t, t.TempDir()+"/state", 64)
	base := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	oldA, _ := reserveHandoffAt(t, r, "a", base)
	oldB, _ := reserveHandoffAt(t, r, "b", base.Add(time.Minute))
	oldDone, doneState := reserveHandoffAt(t, r, "done", base.Add(2*time.Minute))
	recent, _ := reserveHandoffAt(t, r, "recent", base.Add(20*time.Minute))

	connecting := doneState
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	aborted := connecting
	aborted.Phase = handoff.PhaseAborted
	aborted.AuthorityEpoch++
	aborted.DesiredOwner = delegated.OwnerNone
	aborted.ProviderOwner = delegated.OwnerAgent
	if result := r.AdvanceHandoff(context.Background(), aborted); result.Err != nil {
		t.Fatal(result.Err)
	}

	ids, more, err := r.ListExpiredHandoffs(context.Background(), base.Add(10*time.Minute), 1)
	if err != nil || len(ids) != 1 || !more {
		t.Fatalf("first batch ids=%v more=%v err=%v", ids, more, err)
	}
	if ids[0] != oldA.HandoffID && ids[0] != oldB.HandoffID {
		t.Fatalf("unexpected old id=%q", ids[0])
	}

	ids, more, err = r.ListExpiredHandoffs(context.Background(), base.Add(10*time.Minute), 8)
	if err != nil || more || len(ids) != 2 {
		t.Fatalf("full batch ids=%v more=%v err=%v", ids, more, err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[oldA.HandoffID] || !seen[oldB.HandoffID] || seen[oldDone.HandoffID] || seen[recent.HandoffID] {
		t.Fatalf("expired selection=%v", ids)
	}
}
