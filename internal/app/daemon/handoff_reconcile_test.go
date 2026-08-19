package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type reconcileCoordinator struct {
	active    atomic.Int32
	maxActive atomic.Int32
	calls     atomic.Int32
	failID    string
	block     bool
}

func (c *reconcileCoordinator) Reconcile(ctx context.Context, id string) (handoff.State, error) {
	c.calls.Add(1)
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for {
		max := c.maxActive.Load()
		if active <= max || c.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	if id == c.failID {
		return handoff.State{}, failure.New(failure.HandoffReclaimBlocked, map[string]string{"handoff_id": id, "reason": "test", "phase": string(handoff.PhaseHumanOwned)}, nil)
	}
	if c.block {
		<-ctx.Done()
		return handoff.State{}, ctx.Err()
	}
	time.Sleep(5 * time.Millisecond)
	return handoff.State{HandoffID: id}, nil
}
func (*reconcileCoordinator) Request(context.Context, handoff.Request) (handoff.State, error) {
	return handoff.State{}, nil
}
func (*reconcileCoordinator) Wait(context.Context, handoffapp.WaitRequest) (handoffapp.WaitResult, error) {
	return handoffapp.WaitResult{}, nil
}
func (*reconcileCoordinator) Abort(context.Context, string) (handoff.State, error) {
	return handoff.State{}, nil
}
func (*reconcileCoordinator) Inspect(context.Context, string) (handoff.State, error) {
	return handoff.State{}, nil
}
func (*reconcileCoordinator) Expire(context.Context, string) (handoff.State, error) {
	return handoff.State{}, nil
}
func (*reconcileCoordinator) BootstrapLocalHuman(context.Context, string) (handoffapp.LocalBootstrap, error) {
	return handoffapp.LocalBootstrap{}, nil
}
func (*reconcileCoordinator) BindLocalHuman(context.Context, string, delegatedapp.ProviderClientRef) (handoff.State, error) {
	return handoff.State{}, nil
}
func (*reconcileCoordinator) AttachLocalHuman(context.Context, string, delegatedapp.HumanAttachSpec) (handoffapp.LocalAttachResult, error) {
	return handoffapp.LocalAttachResult{}, nil
}
func (*reconcileCoordinator) HumanControl(context.Context, handoff.ControlSignal) (handoffapp.ControlResult, error) {
	return handoffapp.ControlResult{}, nil
}
func (*reconcileCoordinator) ProjectPublic(context.Context, handoff.State) (handoff.PublicState, error) {
	return handoff.PublicState{}, nil
}

func handoffCandidates(n int) []handoff.State {
	out := make([]handoff.State, n)
	for i := range out {
		out[i].HandoffID = "handoff-reconcile-" + string(rune('a'+i))
	}
	return out
}

func TestHandoffStartupReconcileBoundsConcurrencyAndVisitsAll(t *testing.T) {
	coord := &reconcileCoordinator{}
	svc := &Service{handoff: coord}
	if err := svc.ReconcileHandoffStartup(t.Context(), handoffCandidates(6), HandoffStartupOptions{PerHandoff: 100 * time.Millisecond, MaxConcurrency: 2, TotalBudget: time.Second}); err != nil {
		t.Fatal(err)
	}
	if coord.calls.Load() != 6 || coord.maxActive.Load() > 2 {
		t.Fatalf("calls=%d max_active=%d", coord.calls.Load(), coord.maxActive.Load())
	}
}

func TestHandoffStartupReconcileFailureBlocksStartup(t *testing.T) {
	coord := &reconcileCoordinator{failID: "handoff-reconcile-b"}
	svc := &Service{handoff: coord}
	err := svc.ReconcileHandoffStartup(t.Context(), handoffCandidates(3), HandoffStartupOptions{PerHandoff: 100 * time.Millisecond, MaxConcurrency: 2, TotalBudget: time.Second})
	if !errors.Is(err, failure.HandoffReclaimBlocked) {
		t.Fatalf("err=%v", err)
	}
}

func TestHandoffStartupReconcileBudgetIsBoundedAndFailClosed(t *testing.T) {
	coord := &reconcileCoordinator{block: true}
	svc := &Service{handoff: coord}
	started := time.Now()
	err := svc.ReconcileHandoffStartup(t.Context(), handoffCandidates(4), HandoffStartupOptions{PerHandoff: 20 * time.Millisecond, MaxConcurrency: 2, TotalBudget: 35 * time.Millisecond})
	if err == nil {
		t.Fatal("budget exhaustion accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("reconcile exceeded bound: %s", time.Since(started))
	}
}

var _ handoffCoordinator = (*reconcileCoordinator)(nil)
