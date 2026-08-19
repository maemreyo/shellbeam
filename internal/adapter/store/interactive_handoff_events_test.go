package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestHandoffLifecycleObservationsAreExactlyOnceAndMetadataOnly(t *testing.T) {
	t.Run("lifecycle", testHandoffLifecycleEvents)
	t.Run("abort", testHandoffAbortEvent)
}

func testHandoffLifecycleEvents(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, t.TempDir()+"/state", "events")
	if _, created, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffRequested); got != 1 {
		t.Fatalf("requested events=%d", got)
	}

	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	attached := connecting
	attached.ProviderOwner = delegated.OwnerNone
	attached.HumanClient = &handoff.HumanClientRef{Ref: "hclient_events_private"}
	if result := r.AdvanceHandoff(context.Background(), attached); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvanceHandoff(context.Background(), attached); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffAttached); got != 1 {
		t.Fatalf("attached events=%d", got)
	}

	human := attached
	human.Phase = handoff.PhaseHumanOwned
	human.ProviderOwner = delegated.OwnerHuman
	human.HumanIngress = handoff.IngressWritable
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffHumanOwned); got != 1 {
		t.Fatalf("human-owned events=%d", got)
	}

	fencing := human
	fencing.Phase = handoff.PhaseHumanFencing
	fencing.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	if result := r.AdvanceHandoff(context.Background(), fencing); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffReclaimStarted); got != 1 {
		t.Fatalf("reclaim-started events=%d", got)
	}
	fenced := fencing
	fenced.ProviderOwner = delegated.OwnerNone
	fenced.HumanIngress = handoff.IngressFenced
	if result := r.AdvanceHandoff(context.Background(), fenced); result.Err != nil {
		t.Fatal(result.Err)
	}
	agent := fenced
	agent.Phase, agent.DesiredOwner, agent.ProviderOwner = handoff.PhaseAgentOwned, delegated.OwnerAgent, delegated.OwnerAgent
	agent.AuthorityEpoch++
	agent.AgentIngress = handoff.IngressWritable
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffReclaimed); got != 1 {
		t.Fatalf("reclaimed events=%d", got)
	}
	assertHandoffObservationMetadataOnly(t, r)
}

func testHandoffAbortEvent(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, t.TempDir()+"/state", "events-abort")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	aborted := connecting
	aborted.Phase, aborted.DesiredOwner, aborted.ProviderOwner = handoff.PhaseAborted, delegated.OwnerNone, delegated.OwnerNone
	aborted.AuthorityEpoch++
	if result := r.AdvanceHandoff(context.Background(), aborted); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffAborted); got != 1 {
		t.Fatalf("aborted events=%d", got)
	}
	assertHandoffObservationMetadataOnly(t, r)
}

func assertHandoffObservationMetadataOnly(t *testing.T, repo *Repository) {
	t.Helper()
	items, err := repo.ListObservationObligations(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if !strings.HasPrefix(string(item.Kind), "handoff_") {
			continue
		}
		text := item.SubjectRef + " " + item.Summary
		for _, forbidden := range []string{"hclient_", "provider_ref", "provider_generation", "gen-h2", "tmux"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("handoff event leaked %q: %#v", forbidden, item)
			}
		}
	}
}

func TestHandoffPreparedObservationRecoversExactlyOnceFromCrashTransaction(t *testing.T) {
	root := t.TempDir() + "/state"
	r, _, req, initial := h2HandoffFixture(t, root, "event-crash")
	createWrites := 0
	r.writer.fail = func(point string) error {
		if point == "create.write" {
			createWrites++
			// 1 = observation obligation, 2 = handoff transaction,
			// 3 = canonical handoff record after binding rotation.
			if createWrites == 3 {
				return errors.New("inject crash window after binding rotation")
			}
		}
		return nil
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err == nil {
		t.Fatal("faulted handoff reserve unexpectedly succeeded")
	}

	reopened := delegatedRepository(t, root, 64)
	if err := reopened.AbandonUnresolved(context.Background(), "daemon-event-recovery"); err != nil {
		t.Fatal(err)
	}
	if got := countPersistentEventKind(t, reopened, observation.EventHandoffRequested); got != 1 {
		t.Fatalf("recovered requested events=%d", got)
	}
	if _, result := reopened.RecoverHandoff(context.Background(), req.HandoffID); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := reopened.AbandonUnresolved(context.Background(), "daemon-event-recovery-2"); err != nil {
		t.Fatal(err)
	}
	if got := countPersistentEventKind(t, reopened, observation.EventHandoffRequested); got != 1 {
		t.Fatalf("duplicate requested events after recovery=%d", got)
	}
}

func TestHandoffRecoveryFailureEventsAreDistinctAndExactlyOnce(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, t.TempDir()+"/state", "failure-events")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	attached := connecting
	attached.ProviderOwner = delegated.OwnerNone
	attached.HumanClient = &handoff.HumanClientRef{Ref: "hclient_failure_events"}
	if result := r.AdvanceHandoff(context.Background(), attached); result.Err != nil {
		t.Fatal(result.Err)
	}
	human := attached
	human.Phase = handoff.PhaseHumanOwned
	human.ProviderOwner = delegated.OwnerHuman
	human.HumanIngress = handoff.IngressWritable
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}
	lost := human
	lost.Phase = handoff.PhaseHumanConnecting
	lost.ProviderOwner = delegated.OwnerNone
	lost.HumanIngress = handoff.IngressFenced
	lost.HumanClient = nil
	lost.FailureCode = failure.HandoffClientLost
	if result := r.AdvanceHandoff(context.Background(), lost); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvanceHandoff(context.Background(), lost); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffClientLost); got != 1 {
		t.Fatalf("client-lost events=%d", got)
	}

	expired := lost
	expired.AuthorityEpoch++
	expired.DesiredOwner = delegated.OwnerNone
	expired.Phase = handoff.PhaseReclaimPending
	expired.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryNone}
	expired.FailureCode = failure.HandoffExpired
	if result := r.AdvanceHandoff(context.Background(), expired); result.Err != nil {
		t.Fatal(result.Err)
	}
	aborted := expired
	aborted.Phase = handoff.PhaseAborted
	if result := r.AdvanceHandoff(context.Background(), aborted); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := r.AdvanceHandoff(context.Background(), aborted); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffExpired); got != 1 {
		t.Fatalf("expired events=%d", got)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffAborted); got != 0 {
		t.Fatalf("expiry also emitted aborted events=%d", got)
	}
}

func TestHandoffAdvanceTransactionSettlesBeforePreparedObservationRecovery(t *testing.T) {
	root := t.TempDir() + "/state"
	r, _, req, initial := h2HandoffFixture(t, root, "advance-event-crash")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	attached := connecting
	attached.ProviderOwner = delegated.OwnerNone
	attached.HumanClient = &handoff.HumanClientRef{Ref: "hclient_advance_event_crash"}
	if result := r.AdvanceHandoff(context.Background(), attached); result.Err != nil {
		t.Fatal(result.Err)
	}
	human := attached
	human.Phase = handoff.PhaseHumanOwned
	human.ProviderOwner = delegated.OwnerHuman
	human.HumanIngress = handoff.IngressWritable
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}

	agent := human
	agent.Phase = handoff.PhaseAgentOwned
	agent.AuthorityEpoch++
	agent.DesiredOwner = delegated.OwnerAgent
	agent.ProviderOwner = delegated.OwnerAgent
	agent.AgentIngress = handoff.IngressWritable
	agent.HumanIngress = handoff.IngressFenced
	agent.HumanClient = nil
	agent.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	replaceWrites := 0
	r.writer.fail = func(point string) error {
		if point == "replace.write" {
			replaceWrites++
			if replaceWrites == 2 {
				return errors.New("inject state write after binding target")
			}
		}
		return nil
	}
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err == nil {
		t.Fatal("faulted reclaim unexpectedly succeeded")
	}

	reopened := delegatedRepository(t, root, 64)
	if err := reopened.AbandonUnresolved(context.Background(), "daemon-advance-event-recovery"); err != nil {
		t.Fatal(err)
	}
	_, recovered, err := reopened.LoadHandoff(context.Background(), req.HandoffID)
	if err != nil || recovered != agent {
		t.Fatalf("canonical handoff was not settled before event recovery: %#v err=%v", recovered, err)
	}
	if got := countPersistentEventKind(t, reopened, observation.EventHandoffReclaimed); got != 1 {
		t.Fatalf("reclaimed events=%d", got)
	}
	if _, err := os.Stat(reopened.interactiveHandoffTransactionPath(req.HandoffID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction remains after startup recovery: %v", err)
	}
}

func TestHandoffAdvanceRetryCompletesSamePreparedLifecycleEventWithoutRestart(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, t.TempDir()+"/state", "advance-event-retry")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	attached := connecting
	attached.ProviderOwner = delegated.OwnerNone
	attached.HumanClient = &handoff.HumanClientRef{Ref: "hclient_advance_event_retry"}
	if result := r.AdvanceHandoff(context.Background(), attached); result.Err != nil {
		t.Fatal(result.Err)
	}
	human := attached
	human.Phase = handoff.PhaseHumanOwned
	human.ProviderOwner = delegated.OwnerHuman
	human.HumanIngress = handoff.IngressWritable
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}

	agent := human
	agent.Phase = handoff.PhaseAgentOwned
	agent.AuthorityEpoch++
	agent.DesiredOwner = delegated.OwnerAgent
	agent.ProviderOwner = delegated.OwnerAgent
	agent.AgentIngress = handoff.IngressWritable
	agent.HumanIngress = handoff.IngressFenced
	agent.HumanClient = nil
	agent.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	replaceWrites := 0
	r.writer.fail = func(point string) error {
		if point == "replace.write" {
			replaceWrites++
			if replaceWrites == 1 {
				return errors.New("inject binding write failure after transaction")
			}
		}
		return nil
	}
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err == nil {
		t.Fatal("faulted reclaim unexpectedly succeeded")
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffReclaimed); got != 0 {
		t.Fatalf("reclaimed event published before authority target: %d", got)
	}

	r.writer.fail = nil
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got := countPersistentEventKind(t, r, observation.EventHandoffReclaimed); got != 1 {
		t.Fatalf("same-process retry reclaimed events=%d", got)
	}
}
