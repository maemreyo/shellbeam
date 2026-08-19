package daemon_test

import (
	"context"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDelegatedTerminalExitZeroPublishesV5BeforeTerminalVisibility(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-terminal-zero"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	zero := 0
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{
		Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test",
		Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: int64(len("delegated-ready\n")),
	}}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Completed || terminal.Outcome != session.Success || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	rec := terminal.Receipt
	if rec.SchemaVersion != 5 || rec.SessionMode != delegated.ModeDelegatedInteractive || rec.AuthorityEpoch != 1 {
		t.Fatalf("receipt identity=%#v", rec)
	}
	if rec.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly || rec.InputAuthorityProvenance != receipt.InputAuthorityAgentOnly {
		t.Fatalf("receipt authority=%#v", rec)
	}
	if !rec.OutputComplete || rec.CaptureQuality != receipt.CaptureComplete || len(rec.CaptureReasons) != 0 {
		t.Fatalf("capture=%#v", rec)
	}
	if rec.Exit.Code == nil || *rec.Exit.Code != 0 || rec.Exit.Reaped || !rec.Spawn.Attempted || !rec.Spawn.Succeeded {
		t.Fatalf("lifecycle evidence=%#v", rec)
	}
	binding, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if binding.Lifecycle != delegated.LifecycleTerminal || binding.AuthorityEpoch != 1 {
		t.Fatalf("binding=%#v", binding)
	}
	persisted, err := st.LoadReceipt(context.Background(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SchemaVersion != 5 || persisted.State != session.Completed {
		t.Fatalf("persisted=%#v", persisted)
	}
}

func TestDelegatedProviderLossPublishesAmbiguousV5WithoutInventingChildExit(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-provider-loss"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	runtime.waitCh <- delegatedWaitResult{err: failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": started.SessionID, "provider_id": "tmux_control_mode", "reason": "observer_lost"}, nil)}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Abandoned || terminal.Outcome != session.Ambiguous || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	rec := terminal.Receipt
	if rec.Exit.Reaped || rec.Exit.Code != nil {
		t.Fatalf("provider loss invented child exit: %#v", rec.Exit)
	}
	if rec.OutputComplete || rec.CaptureQuality != receipt.CaptureIncomplete || rec.FailureReason != "provider_lost" {
		t.Fatalf("loss receipt=%#v failure=%#v", rec, rec.Failure())
	}
	if len(rec.CaptureReasons) != 1 || rec.CaptureReasons[0] != receipt.CaptureReasonProviderLost {
		t.Fatalf("capture reasons=%v", rec.CaptureReasons)
	}
	binding, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if binding.Lifecycle != delegated.LifecycleLost {
		t.Fatalf("binding=%#v", binding)
	}
}

type gatedDelegatedTerminalStore struct {
	*storeadapter.Repository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedDelegatedTerminalStore) PublishTerminal(ctx context.Context, rec receipt.Receipt) app.StoreResult {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
		return s.Repository.PublishTerminal(ctx, rec)
	case <-ctx.Done():
		return app.StoreResult{Err: ctx.Err()}
	}
}

func TestDelegatedTerminalReceiptIsDurableBeforeTerminalView(t *testing.T) {
	repo := openDelegatedStartStore(t)
	store := &gatedDelegatedTerminalStore{Repository: repo, entered: make(chan struct{}), release: make(chan struct{})}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-terminal-gate"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: 16}}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("terminal publish not reached")
	}
	view, err := svc.Poll(context.Background(), app.PollRequest{SessionID: started.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if view.State != session.Finalizing || view.Receipt != nil {
		t.Fatalf("pre-durable view=%#v", view)
	}
	close(store.release)
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Completed || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
}

func TestDelegatedTransportGapNeverClaimsSuccessfulCompleteOutput(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-transport-gap"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: 15}}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Failed || terminal.Outcome != session.Failure || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	rec := terminal.Receipt
	if rec.OutputComplete || rec.CaptureQuality != receipt.CaptureIncomplete || len(rec.CaptureReasons) != 1 || rec.CaptureReasons[0] != receipt.CaptureReasonTransportGap || rec.FailureReason != "output_capture_failed" {
		t.Fatalf("receipt=%#v", rec)
	}
}

func TestDelegatedNonzeroExitPreservesExitStatusWithCompleteCapture(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-exit-seven"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	seven := 7
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &seven, OutputBytes: 16}}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Failed || terminal.Outcome != session.Failure || terminal.Receipt == nil || terminal.Receipt.Exit.Code == nil || *terminal.Receipt.Exit.Code != 7 {
		t.Fatalf("terminal=%#v", terminal)
	}
	if !terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CaptureComplete {
		t.Fatalf("receipt=%#v", terminal.Receipt)
	}
}

func TestDelegatedKillTerminalPreservesKillClassificationAndSignalEvidence(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-kill-terminal"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Kill(context.Background(), app.KillRequest{SessionID: started.SessionID, AuthorityEpoch: 1, KillID: "kill-terminal", Signal: "TERM"}); err != nil {
		t.Fatal(err)
	}
	code := 143
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &code, OutputBytes: 16}}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Killed || terminal.Outcome != session.KilledOutcome || terminal.Receipt == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.Signal.Requested != "TERM" || !terminal.Receipt.Signal.Attempted || !terminal.Receipt.Signal.Succeeded {
		t.Fatalf("signal=%#v", terminal.Receipt.Signal)
	}
}

func TestDelegatedCreateResponseLossPreservesPreAckOutputAccountingThroughTerminal(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newAmbiguousCreateRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-pre-ack-output"
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("first ambiguous create unexpectedly succeeded")
	}
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	wantOutput := "ambiguous-pre-ack\ndelegated-ready\n"
	if started.Output != wantOutput {
		t.Fatalf("start output=%q want=%q", started.Output, wantOutput)
	}
	zero := 0
	runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: int64(len(wantOutput))}}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.State != session.Completed || terminal.Receipt == nil || !terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CaptureComplete {
		t.Fatalf("terminal=%#v", terminal)
	}
}
