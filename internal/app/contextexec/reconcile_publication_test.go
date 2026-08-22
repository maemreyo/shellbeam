package contextexec

import (
	"context"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func recoveredTerminalFixture(t *testing.T) (*Service, *admissionStoreFake, *terminalSchedulerFake, operation.ContextExecState) {
	t.Helper()
	req := admissionRequest()
	authenticated := helperAuthenticatedState(t, req)
	store := &admissionStoreFake{state: authenticated, found: true}
	scheduler := &terminalSchedulerFake{}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6", TerminalScheduler: scheduler})
	authorized, auth, err := svc.AuthorizePrepared(context.Background(), authenticated, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := svc.RecordSpawn(context.Background(), authorized, SpawnTruth{ChildOperationID: auth.ChildOperationID, ChildSessionID: auth.ChildSessionID, ResolvedExecutable: auth.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}})
	if err != nil {
		t.Fatal(err)
	}
	terminal := validChildTerminalResult(t, spawned)
	terminal.Output.StdoutBytes = 1
	childTerminal := spawned.Clone()
	childTerminal.Lifecycle = core.LifecycleChildTerminal
	childTerminal.Result = &terminal
	childTerminal.UpdatedAt = childTerminal.UpdatedAt.Add(1)
	if err := childTerminal.Validate(); err != nil {
		t.Fatal(err)
	}
	store.state = childTerminal.Clone()
	store.output = []byte("x")
	store.recoveryCandidates = []operation.ContextExecState{childTerminal.Clone()}
	return svc, store, scheduler, childTerminal
}

func TestReconcileChildTerminalRepairsCanonicalReceiptThenReleasesLease(t *testing.T) {
	svc, store, scheduler, _ := recoveredTerminalFixture(t)
	events := []string{}
	store.events = &events
	scheduler.events = &events
	decisions, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Disposition != RecoveryFinal || decisions[0].State.Lifecycle != core.LifecycleCanonicalized || decisions[0].RetainLease {
		t.Fatalf("decisions=%#v", decisions)
	}
	want := "reserve_operation,advance_canonicalized,publish_receipt,schedule_terminal,release_lease"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("events=%q want=%q", got, want)
	}
	if scheduler.calls != 1 || store.releaseCalls != 1 || store.publishedReceipt.SchemaVersion != 6 {
		t.Fatalf("scheduler=%d release=%d receipt=%#v", scheduler.calls, store.releaseCalls, store.publishedReceipt)
	}
}

func TestReconcileCanonicalizedSuccessRepublishesBeforeLeaseRelease(t *testing.T) {
	svc, store, scheduler, terminal := recoveredTerminalFixture(t)
	canonical := *terminal.Result
	canonical.Lifecycle = core.LifecycleCanonicalized
	canonical.EvidenceAuthority = core.EvidenceAuthorityContextExecChildOwnedV1
	terminal.Lifecycle = core.LifecycleCanonicalized
	terminal.Result = &canonical
	terminal.UpdatedAt = terminal.UpdatedAt.Add(1)
	if err := terminal.Validate(); err != nil {
		t.Fatal(err)
	}
	store.state = terminal.Clone()
	store.recoveryCandidates = []operation.ContextExecState{terminal.Clone()}
	events := []string{}
	store.events = &events
	scheduler.events = &events
	decisions, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Disposition != RecoveryFinal || decisions[0].RetainLease {
		t.Fatalf("decisions=%#v", decisions)
	}
	want := "reserve_operation,publish_receipt,schedule_terminal,release_lease"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("events=%q want=%q", got, want)
	}
}
