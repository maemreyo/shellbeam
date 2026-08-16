package daemon_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// vanishingReceiptStore reproduces the exact interleaving retention can
// produce, deterministically instead of by racing a real sweep.
//
// A snapshot read and the receipt read that follows it are two separate,
// unsynchronized calls, and retention withdraws a session as a single atomic
// rename between them: a snapshot read landing moments before the rename finds
// a terminal session, and the receipt read that follows it lands after the
// record is gone. LoadSession here succeeds once, as it would moments before
// the rename, and reports the record gone on every call after, as it would
// once the rename has landed; LoadReceipt always reports it gone, standing in
// for the read that lost the race.
type vanishingReceiptStore struct {
	*storeadapter.Repository
	sessionReads atomic.Int32
}

func (s *vanishingReceiptStore) LoadSession(ctx context.Context, id operation.SessionID) (session.Snapshot, error) {
	if s.sessionReads.Add(1) == 1 {
		return session.Snapshot{
			SchemaVersion: 1, OperationID: "vanishing-op", SessionID: string(id),
			State: session.Completed, Outcome: session.Success, UpdatedAt: time.Now().UTC(),
		}, nil
	}
	return session.Snapshot{}, storeadapter.ErrNotFound
}

func (s *vanishingReceiptStore) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	return receipt.Receipt{}, storeadapter.ErrNotFound
}

// TestPollDoesNotReturnATerminalStateWithNoReceipt is the production-facing
// half of the retention race: a snapshot read that finds a terminal session,
// followed by a receipt read that has lost the race against a GC rename, used
// to produce state: completed with no receipt and no error -- a terminal
// answer with no evidence behind it. A terminal snapshot's receipt is always
// durably published before the snapshot is ever marked terminal, so a missing
// receipt here means the record is vanishing, not that it never had one; the
// caller is told the session is gone rather than being handed a state it
// cannot substantiate.
func TestPollDoesNotReturnATerminalStateWithNoReceipt(t *testing.T) {
	store := &vanishingReceiptStore{Repository: lifecycleRepository(t)}
	svc := app.NewService(store, nil, app.Options{
		Incarnation: "race", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		DefaultTimeoutMS: 600000, MaxTimeoutMS: 86400000,
	})

	view, err := svc.Poll(context.Background(), app.PollRequest{SessionID: "vanishing-session", MaxOutputBytes: 64})
	if err == nil {
		t.Fatalf("expected a refusal, got a view: %#v", view)
	}
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Code != failure.SessionNotFound {
		t.Fatalf("refusal = %v, want %q", err, failure.SessionNotFound)
	}
	if view.State != "" || view.Receipt != nil {
		t.Fatalf("a refusal must not carry a partial view: %#v", view)
	}
}
