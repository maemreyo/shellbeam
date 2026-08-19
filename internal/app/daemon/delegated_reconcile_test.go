package daemon_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type restartDelegatedRuntime struct {
	*delegatedStartRuntime
	reattachObs    delegatedapp.Observation
	reattachErr    error
	reattachOutput string
	reattachCalls  atomic.Int32
	detachCalls    atomic.Int32
	closeCalls     atomic.Int32
}

func newRestartDelegatedRuntime() *restartDelegatedRuntime {
	return &restartDelegatedRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}
}

func (r *restartDelegatedRuntime) Reattach(_ context.Context, _ delegated.ProviderRef, sink delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	r.reattachCalls.Add(1)
	if r.reattachOutput != "" && sink != nil {
		if err := sink.Append([]byte(r.reattachOutput)); err != nil {
			return delegatedapp.Observation{}, err
		}
	}
	obs := r.reattachObs
	if obs.Provider.ID == "" {
		obs.Provider = r.Identity()
	}
	if obs.ProviderGeneration == "" && r.reattachErr == nil {
		obs.ProviderGeneration = "gen_restart"
	}
	obs.OutputBytes = int64(len(r.reattachOutput))
	return obs, r.reattachErr
}

func (r *restartDelegatedRuntime) Detach(context.Context, delegated.ProviderRef) error {
	r.detachCalls.Add(1)
	return nil
}

func (r *restartDelegatedRuntime) Close(context.Context, delegated.ProviderRef) error {
	r.closeCalls.Add(1)
	return nil
}

func reserveDelegatedRecovery(t *testing.T, st *storeadapter.Repository, suffix string, lifecycle delegated.Lifecycle, initialOutput string, nextOffset int64) (delegated.Binding, delegated.ProviderRef) {
	t.Helper()
	now := time.Date(2026, 8, 19, 2, 40, 0, 0, time.UTC)
	sid := "delegated-restart-" + suffix
	opID := "delegated-restart-" + suffix + "-op"
	reservation := operation.Reservation{
		SchemaVersion: 5, OperationID: operation.ID(opID), SessionID: operation.SessionID(sid),
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "cat", CWD: "/tmp", Shell: "/bin/sh",
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DaemonIncarnation: "old-daemon", CreatedAt: now,
	}
	if _, created, got := st.ReserveOperation(context.Background(), reservation); got.Err != nil || !created {
		t.Fatalf("reserve operation created=%v result=%#v", created, got)
	}
	binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: sid, OperationID: opID, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: delegated.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: sid, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: "dtmux_restart_" + suffix, CreatedAt: now, UpdatedAt: now}
	stored, created, got := st.ReserveDelegatedBinding(context.Background(), binding, ref)
	if got.Err != nil || !created {
		t.Fatalf("reserve binding created=%v result=%#v", created, got)
	}
	if lifecycle == delegated.LifecycleLive {
		stored.Lifecycle = delegated.LifecycleLive
		stored.UpdatedAt = now.Add(time.Nanosecond)
		if got := st.AdvanceDelegatedBinding(context.Background(), stored); got.Err != nil {
			t.Fatal(got.Err)
		}
		binding = stored
		if got := st.AdvanceSession(context.Background(), session.Snapshot{SchemaVersion: 1, OperationID: opID, SessionID: sid, DaemonIncarnation: "old-daemon", State: session.Running, OutputAvailable: true, UpdatedAt: now.Add(time.Nanosecond)}); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	if initialOutput != "" {
		if _, got := st.AppendOutput(context.Background(), operation.SessionID(sid), []byte(initialOutput)); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	if nextOffset > 0 {
		id := delegated.MutationIdentity{SessionID: sid, Epoch: 1, Kind: delegated.MutationWrite, Offset: 0, NextOffset: nextOffset, Fingerprint: "fp-restart-write"}
		if _, _, got := st.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
			t.Fatal(got.Err)
		}
		if _, got := st.CompleteDelegatedMutation(context.Background(), id, delegated.MutationCompleted, "succeeded"); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	return binding, ref
}

func TestDelegatedRestartReconcileReattachesSameSessionEpochOffsetAndGracefulShutdownDetaches(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "live", delegated.LifecycleLive, "old", 3)
	binding.AuthorityEpoch = 3
	binding.UpdatedAt = binding.UpdatedAt.Add(time.Second)
	if got := st.AdvanceDelegatedBinding(context.Background(), binding); got.Err != nil {
		t.Fatal(got.Err)
	}
	runtime := newRestartDelegatedRuntime()
	runtime.reattachObs = delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_restart", Owner: delegated.OwnerAgent}
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	if err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.reattachCalls.Load() != 1 || runtime.creates.Load() != 0 {
		t.Fatalf("reattach=%d creates=%d", runtime.reattachCalls.Load(), runtime.creates.Load())
	}
	view, err := svc.Poll(context.Background(), app.PollRequest{SessionID: binding.SessionID, MaxOutputBytes: 64})
	if err != nil || view.State != session.Running || view.AuthorityEpoch != binding.AuthorityEpoch || view.NextInputOffset != 3 || view.Output != "old" {
		t.Fatalf("view=%#v err=%v", view, err)
	}
	written, err := svc.Write(context.Background(), app.WriteRequest{SessionID: binding.SessionID, AuthorityEpoch: binding.AuthorityEpoch, InputOffset: 3, Chars: "xy"})
	if err != nil || written.NextInputOffset != 5 || runtime.writes.Load() != 1 {
		t.Fatalf("write=%#v err=%v provider_writes=%d", written, err, runtime.writes.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.detachCalls.Load() != 1 || runtime.closeCalls.Load() != 0 || runtime.signals.Load() != 0 {
		t.Fatalf("detach=%d close=%d signals=%d", runtime.detachCalls.Load(), runtime.closeCalls.Load(), runtime.signals.Load())
	}
	stored, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != delegated.LifecycleLive || stored.AuthorityEpoch != binding.AuthorityEpoch {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	candidates, err := st.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 1 || candidates[0].SessionID != binding.SessionID {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

func TestDelegatedRestartReconcileProviderAbsentPublishesCanonicalLostWithoutRecreate(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "lost", delegated.LifecycleLive, "old", 0)
	runtime := newRestartDelegatedRuntime()
	runtime.reattachErr = failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": binding.SessionID, "provider_id": "tmux_control_mode", "reason": "private_state_missing"}, nil)
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	if err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	if runtime.creates.Load() != 0 {
		t.Fatalf("provider recreated: %d", runtime.creates.Load())
	}
	rec, err := st.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "provider_lost" || rec.OutputComplete || rec.CaptureQuality != receipt.CaptureIncomplete {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
	if len(rec.CaptureReasons) != 2 || rec.CaptureReasons[0] != receipt.CaptureReasonProviderLost || rec.CaptureReasons[1] != receipt.CaptureReasonTransportGap || rec.OutputBytes != 3 {
		t.Fatalf("capture=%#v", rec)
	}
	stored, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != delegated.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
}

func TestDelegatedRestartReconcileProviderMismatchBlocksAndPreservesRecoveryMarker(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "mismatch", delegated.LifecycleLive, "", 0)
	runtime := newRestartDelegatedRuntime()
	runtime.reattachErr = failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": binding.SessionID, "provider_id": "tmux_control_mode"}, errors.New("generation mismatch"))
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{})
	if !errors.Is(err, failure.DelegatedReconcileBlocked) || runtime.creates.Load() != 0 {
		t.Fatalf("err=%v creates=%d", err, runtime.creates.Load())
	}
	if _, err := st.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID)); err == nil {
		t.Fatal("mismatch invented terminal receipt")
	}
	stored, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != delegated.LifecycleLive {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	candidates, err := st.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 1 || candidates[0].SessionID != binding.SessionID {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

type corruptDelegatedRefStore struct {
	*storeadapter.Repository
}

func (s *corruptDelegatedRefStore) LoadDelegatedProviderRef(context.Context, operation.SessionID) (delegated.ProviderRef, error) {
	return delegated.ProviderRef{}, errors.New("corrupt private ref")
}

func TestDelegatedRestartReconcileUnresolvedMutationBlocksBeforeProviderTouch(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "unresolved", delegated.LifecycleLive, "", 0)
	id := delegated.MutationIdentity{SessionID: binding.SessionID, Epoch: binding.AuthorityEpoch, Kind: delegated.MutationWrite, Offset: 0, NextOffset: 2, Fingerprint: "fp-restart-unresolved"}
	if _, _, got := st.ReserveDelegatedMutation(context.Background(), id); got.Err != nil {
		t.Fatal(got.Err)
	}
	runtime := newRestartDelegatedRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{})
	if !errors.Is(err, failure.DelegatedReconcileBlocked) || runtime.reattachCalls.Load() != 0 || runtime.creates.Load() != 0 {
		t.Fatalf("err=%v reattach=%d creates=%d", err, runtime.reattachCalls.Load(), runtime.creates.Load())
	}
}

func TestDelegatedRestartReconcileCorruptPrivateRefBlocksBeforeProviderTouch(t *testing.T) {
	repo := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, repo, "corrupt-ref", delegated.LifecycleLive, "", 0)
	store := &corruptDelegatedRefStore{Repository: repo}
	runtime := newRestartDelegatedRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{})
	if !errors.Is(err, failure.DelegatedReconcileBlocked) || runtime.reattachCalls.Load() != 0 || runtime.creates.Load() != 0 {
		t.Fatalf("err=%v reattach=%d creates=%d", err, runtime.reattachCalls.Load(), runtime.creates.Load())
	}
	candidates, err := repo.ListDelegatedRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 1 || candidates[0].SessionID != binding.SessionID {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

type blockingRestartRuntime struct {
	*restartDelegatedRuntime
	active    atomic.Int32
	maxActive atomic.Int32
}

func newBlockingRestartRuntime() *blockingRestartRuntime {
	return &blockingRestartRuntime{restartDelegatedRuntime: newRestartDelegatedRuntime()}
}

func (r *blockingRestartRuntime) Reattach(ctx context.Context, _ delegated.ProviderRef, _ delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	r.reattachCalls.Add(1)
	active := r.active.Add(1)
	for {
		max := r.maxActive.Load()
		if active <= max || r.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	defer r.active.Add(-1)
	<-ctx.Done()
	return delegatedapp.Observation{}, ctx.Err()
}

func TestDelegatedRestartReconcileBoundsConcurrencyAndLeavesBudgetUncertaintyFenced(t *testing.T) {
	st := openDelegatedStartStore(t)
	bindings := make([]delegated.Binding, 0, 4)
	for i := 0; i < 4; i++ {
		binding, _ := reserveDelegatedRecovery(t, st, "budget-"+string(rune('a'+i)), delegated.LifecycleLive, "", 0)
		bindings = append(bindings, binding)
	}
	runtime := newBlockingRestartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	started := time.Now()
	err := svc.ReconcileDelegatedStartup(context.Background(), bindings, app.DelegatedStartupOptions{PerSession: 10 * time.Millisecond, MaxConcurrency: 2, TotalBudget: 18 * time.Millisecond})
	if !errors.Is(err, failure.DelegatedReconcileBlocked) {
		t.Fatalf("err=%v want delegated_reconcile_blocked", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("startup reconciliation exceeded bounded budget: %s", elapsed)
	}
	if runtime.maxActive.Load() > 2 || runtime.creates.Load() != 0 {
		t.Fatalf("max_active=%d creates=%d", runtime.maxActive.Load(), runtime.creates.Load())
	}
	candidates, loadErr := st.ListDelegatedRecoveryCandidates(context.Background())
	if loadErr != nil || len(candidates) != len(bindings) {
		t.Fatalf("candidates=%d/%d err=%v", len(candidates), len(bindings), loadErr)
	}
}

func TestDelegatedRestartReconcileTerminalDuringAbsencePublishesOnlyProvableExitWithTransportGap(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "terminal", delegated.LifecycleLive, "old", 0)
	runtime := newRestartDelegatedRuntime()
	zero := 0
	runtime.reattachOutput = "tail"
	runtime.reattachObs = delegatedapp.Observation{Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_restart", Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero}
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	if err := svc.ReconcileDelegatedStartup(context.Background(), []delegated.Binding{binding}, app.DelegatedStartupOptions{}); err != nil {
		t.Fatal(err)
	}
	rec, err := st.LoadReceipt(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Exit.Code == nil || *rec.Exit.Code != 0 || rec.Exit.Reaped || rec.State != session.Completed || rec.Outcome != session.Success || rec.FailureReason != "" {
		t.Fatalf("terminal truth=%#v", rec)
	}
	if rec.OutputComplete || rec.CaptureQuality != receipt.CaptureIncomplete || len(rec.CaptureReasons) != 1 || rec.CaptureReasons[0] != receipt.CaptureReasonTransportGap || rec.OutputBytes != 7 {
		t.Fatalf("capture=%#v", rec)
	}
	stored, err := st.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || stored.Lifecycle != delegated.LifecycleTerminal || runtime.creates.Load() != 0 || runtime.closeCalls.Load() != 1 {
		t.Fatalf("binding=%#v err=%v creates=%d close=%d", stored, err, runtime.creates.Load(), runtime.closeCalls.Load())
	}
}
