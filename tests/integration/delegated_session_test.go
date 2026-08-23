//go:build darwin

package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestH1DelegatedNativeMatrixResponseLossEpochAndProviderLoss(t *testing.T) {
	matrix := newH1NativeMatrix(t)
	started, reservation := h1AssertResponseLossReplay(t, matrix)
	h1AssertEpochReplayAndCleanTerminal(t, matrix, started, reservation)
	h1AssertProviderLossFailsClosed(t, matrix)
}

func TestH1OrdinaryDirectAndPersistentPathsPayZeroDelegatedProviderTax(t *testing.T) {
	store := openH1Store(t, filepath.Join(t.TempDir(), "state"))
	tripwire := &h1TripwireDelegatedProvider{}
	owner := &h1ImmediateOwner{}
	persistentRuntime := &h1PersistentRuntime{store: store}
	svc := daemonapp.NewService(store, owner, daemonapp.Options{
		Incarnation: "h1-no-tax", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: tripwire, PersistentRuntime: persistentRuntime,
	})

	direct, err := svc.Start(t.Context(), daemonapp.StartRequest{ProtocolVersion: 2, OperationID: "h1-direct-no-tax", Command: "true", CWD: "/tmp", YieldMS: 25})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitH1Terminal(t, svc, direct.SessionID)
	if owner.starts.Load() != 1 || tripwire.calls.Load() != 0 {
		t.Fatalf("direct owner_starts=%d delegated_calls=%d", owner.starts.Load(), tripwire.calls.Load())
	}

	persistentView, err := svc.Start(t.Context(), daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "h1-persistent-no-tax", Command: "sleep 10", CWD: "/tmp",
		Persistent: true, SessionName: "h1-no-tax-persistent", YieldMS: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if persistentView.State != session.Running || persistentRuntime.calls.Load() != 1 || tripwire.calls.Load() != 0 {
		t.Fatalf("persistent=%#v persistent_calls=%d delegated_calls=%d", persistentView, persistentRuntime.calls.Load(), tripwire.calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if tripwire.calls.Load() != 0 {
		t.Fatalf("shutdown touched delegated provider for ordinary/B1 sessions: %d", tripwire.calls.Load())
	}
}
