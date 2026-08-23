package daemon_test

import (
	"context"
	"errors"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func startDelegatedControlSession(t *testing.T) (*app.Service, *delegatedStartRuntime, interface {
	LoadDelegatedBinding(context.Context, operation.SessionID) (delegated.Binding, error)
	AdvanceDelegatedBinding(context.Context, delegated.Binding) app.StoreResult
}, string) {
	t.Helper()
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-control-" + t.Name()
	view, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return svc, runtime, st, view.SessionID
}

func rotateDelegatedEpoch(t *testing.T, store interface {
	LoadDelegatedBinding(context.Context, operation.SessionID) (delegated.Binding, error)
	AdvanceDelegatedBinding(context.Context, delegated.Binding) app.StoreResult
}, sid string) delegated.Binding {
	t.Helper()
	binding, err := store.LoadDelegatedBinding(context.Background(), operation.SessionID(sid))
	if err != nil {
		t.Fatal(err)
	}
	binding.AuthorityEpoch++
	binding.UpdatedAt = binding.UpdatedAt.Add(time.Second)
	result := store.AdvanceDelegatedBinding(context.Background(), binding)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	return binding
}

func TestDelegatedWriteReplaysKnownEpochOneAfterEpochTwoAndRejectsUnseenStaleWrite(t *testing.T) {
	svc, runtime, store, sid := startDelegatedControlSession(t)
	firstReq := app.WriteRequest{SessionID: sid, AuthorityEpoch: 1, InputOffset: 0, Chars: "abc"}
	first, err := svc.Write(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	if first.AuthorityEpoch != 1 || first.AcceptedInputBytes != 3 || first.NextInputOffset != 3 || runtime.writes.Load() != 1 || runtime.inspects.Load() != 1 {
		t.Fatalf("first=%#v writes=%d inspects=%d", first, runtime.writes.Load(), runtime.inspects.Load())
	}

	rotateDelegatedEpoch(t, store, sid)
	replayed, err := svc.Write(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.AuthorityEpoch != 1 || replayed.AcceptedInputBytes != first.AcceptedInputBytes || replayed.NextInputOffset != first.NextInputOffset || runtime.writes.Load() != 1 || runtime.inspects.Load() != 1 {
		t.Fatalf("replay=%#v writes=%d inspects=%d", replayed, runtime.writes.Load(), runtime.inspects.Load())
	}

	_, err = svc.Write(context.Background(), app.WriteRequest{SessionID: sid, AuthorityEpoch: 1, InputOffset: 3, Chars: "x"})
	if !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("stale unseen write err=%v", err)
	}
	if runtime.writes.Load() != 1 {
		t.Fatalf("stale write delivered: %d", runtime.writes.Load())
	}
}

func TestDelegatedKillReplaysKnownEpochOneAfterEpochTwoAndRejectsUnseenStaleKill(t *testing.T) {
	svc, runtime, store, sid := startDelegatedControlSession(t)
	firstReq := app.KillRequest{SessionID: sid, AuthorityEpoch: 1, KillID: "kill-1", Signal: "TERM"}
	first, err := svc.Kill(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	if first.AuthorityEpoch != 1 || first.KillID != "kill-1" || !first.SignalAttempt.Attempted || !first.SignalAttempt.Succeeded || runtime.signals.Load() != 1 || runtime.inspects.Load() != 1 {
		t.Fatalf("first=%#v signals=%d inspects=%d", first, runtime.signals.Load(), runtime.inspects.Load())
	}

	rotateDelegatedEpoch(t, store, sid)
	replayed, err := svc.Kill(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.AuthorityEpoch != 1 || replayed.KillID != first.KillID || replayed.Signal != first.Signal || runtime.signals.Load() != 1 || runtime.inspects.Load() != 1 {
		t.Fatalf("replay=%#v signals=%d inspects=%d", replayed, runtime.signals.Load(), runtime.inspects.Load())
	}

	_, err = svc.Kill(context.Background(), app.KillRequest{SessionID: sid, AuthorityEpoch: 1, KillID: "kill-2", Signal: "TERM"})
	if !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("stale unseen kill err=%v", err)
	}
	if runtime.signals.Load() != 1 {
		t.Fatalf("stale kill delivered: %d", runtime.signals.Load())
	}
}

type recordingDelegatedMutationStore struct {
	*storeadapter.Repository
	reserves atomic.Int32
}

func (s *recordingDelegatedMutationStore) ReserveDelegatedMutation(ctx context.Context, id delegated.MutationIdentity) (delegated.MutationRecord, bool, app.StoreResult) {
	s.reserves.Add(1)
	return s.Repository.ReserveDelegatedMutation(ctx, id)
}

func TestDelegatedWriteInvalidOffsetDoesNotReserveMutationOrTouchProvider(t *testing.T) {
	repository := openDelegatedStartStore(t)
	store := &recordingDelegatedMutationStore{Repository: repository}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-bad-offset"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Write(context.Background(), app.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 1, InputOffset: 7, Chars: "x"})
	if !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("err=%v want operation_conflict", err)
	}
	if store.reserves.Load() != 0 {
		t.Fatalf("invalid offset reserved %d mutations", store.reserves.Load())
	}
	if runtime.inspects.Load() != 0 || runtime.writes.Load() != 0 {
		t.Fatalf("invalid offset touched provider inspects=%d writes=%d", runtime.inspects.Load(), runtime.writes.Load())
	}
}

type ambiguousWriteRuntime struct {
	*delegatedStartRuntime
	writeCalls atomic.Int32
}

func (r *ambiguousWriteRuntime) Write(context.Context, delegated.ProviderRef, []byte) error {
	r.writeCalls.Add(1)
	return failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": "ambiguous-write", "provider_id": "tmux_control_mode", "reason": "write_ack_lost"}, nil)
}

func TestDelegatedWriteProviderAmbiguityPersistsUnknownAndRetryInspectsWithoutRedelivery(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := &ambiguousWriteRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-write-ambiguity"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	write := app.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 1, InputOffset: 0, Chars: "abc"}
	if _, err := svc.Write(context.Background(), write); !errors.Is(err, failure.DelegatedProviderLost) {
		t.Fatalf("first err=%v", err)
	}
	if runtime.writeCalls.Load() != 1 || runtime.inspects.Load() != 1 {
		t.Fatalf("first calls writes=%d inspects=%d", runtime.writeCalls.Load(), runtime.inspects.Load())
	}
	if _, err := svc.Write(context.Background(), write); !errors.Is(err, failure.DelegatedReconcileBlocked) {
		t.Fatalf("retry err=%v", err)
	}
	if runtime.writeCalls.Load() != 1 || runtime.inspects.Load() != 2 {
		t.Fatalf("retry redelivered writes=%d inspects=%d", runtime.writeCalls.Load(), runtime.inspects.Load())
	}
}

func TestDelegatedEOFIsRejectedBeforeMutationReserveOrProvider(t *testing.T) {
	repository := openDelegatedStartStore(t)
	store := &recordingDelegatedMutationStore{Repository: repository}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-eof-h1"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Write(context.Background(), app.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 1, InputOffset: 0, EOF: true})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v want invalid_input", err)
	}
	if store.reserves.Load() != 0 {
		t.Fatalf("EOF reserved %d mutations", store.reserves.Load())
	}
	if runtime.inspects.Load() != 0 || runtime.writes.Load() != 0 {
		t.Fatalf("EOF touched provider inspects=%d writes=%d", runtime.inspects.Load(), runtime.writes.Load())
	}
}

func TestHumanDesiredOwnerRejectsUnseenAgentWriteAndKillBeforeReserveOrProvider(t *testing.T) {
	repository := openDelegatedStartStore(t)
	store := &recordingDelegatedMutationStore{Repository: repository}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	start := delegatedStartRequest()
	start.OperationID = "op-human-owned-agent-deny"
	view, err := svc.Start(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repository.LoadDelegatedBinding(t.Context(), operation.SessionID(view.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	binding.AuthorityEpoch++
	binding.DesiredOwner = delegated.OwnerHuman
	binding.UpdatedAt = binding.UpdatedAt.Add(time.Second)
	if result := repository.AdvanceDelegatedBinding(t.Context(), binding); result.Err != nil {
		t.Fatal(result.Err)
	}
	beforeInspect := runtime.inspects.Load()
	if _, err := svc.Write(t.Context(), app.WriteRequest{SessionID: view.SessionID, AuthorityEpoch: binding.AuthorityEpoch, InputOffset: 0, Chars: "x"}); !errors.Is(err, failure.SessionControlNotOwned) {
		t.Fatalf("write err=%v", err)
	}
	if _, err := svc.Kill(t.Context(), app.KillRequest{SessionID: view.SessionID, AuthorityEpoch: binding.AuthorityEpoch, KillID: "kill-human-owned", Signal: "TERM"}); !errors.Is(err, failure.SessionControlNotOwned) {
		t.Fatalf("kill err=%v", err)
	}
	if store.reserves.Load() != 0 {
		t.Fatalf("human-owned controls reserved mutations=%d", store.reserves.Load())
	}
	if runtime.inspects.Load() != beforeInspect || runtime.writes.Load() != 0 || runtime.signals.Load() != 0 {
		t.Fatalf("human-owned controls touched provider inspects %d->%d writes=%d signals=%d", beforeInspect, runtime.inspects.Load(), runtime.writes.Load(), runtime.signals.Load())
	}
}
