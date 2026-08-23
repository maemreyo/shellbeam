package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestHumanControlLedgerReplaysOldEpochAndRejectsConflicts(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "control")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	human := initial
	human.Phase = handoff.PhaseHumanOwned
	human.ProviderOwner = delegated.OwnerHuman
	human.AgentIngress = handoff.IngressFenced
	human.HumanIngress = handoff.IngressWritable
	human.HumanClient = &handoff.HumanClientRef{Ref: "human-client-control"}
	human.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}

	signal := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: human.AuthorityEpoch, ControlID: "control-ready-1", Kind: handoff.HumanControlReady}
	stored, outcome, created, result := r.ReserveControlSignal(context.Background(), signal)
	if result.Err != nil || !created || stored != signal || outcome != "" {
		t.Fatalf("reserve signal=%#v outcome=%q created=%v result=%#v", stored, outcome, created, result)
	}
	if got, result := r.CompleteControlSignal(context.Background(), signal, "ready_accepted"); result.Err != nil || got != "ready_accepted" {
		t.Fatalf("complete outcome=%q result=%#v", got, result)
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
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err != nil {
		t.Fatal(result.Err)
	}

	replay, outcome, created, result := r.ReserveControlSignal(context.Background(), signal)
	if result.Err != nil || created || replay != signal || outcome != "ready_accepted" {
		t.Fatalf("old replay=%#v outcome=%q created=%v result=%#v", replay, outcome, created, result)
	}
	changed := signal
	changed.Kind = handoff.HumanControlAbort
	if _, _, _, result := r.ReserveControlSignal(context.Background(), changed); !errors.Is(result.Err, failure.HandoffConflict) {
		t.Fatalf("same id changed kind err=%v", result.Err)
	}
	stale := signal
	stale.ControlID = "control-stale-new"
	if _, _, _, result := r.ReserveControlSignal(context.Background(), stale); !errors.Is(result.Err, failure.StaleControlGeneration) {
		t.Fatalf("unseen stale err=%v", result.Err)
	}
	impossible := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: agent.AuthorityEpoch, ControlID: "control-ready-agent", Kind: handoff.HumanControlReady}
	if _, _, _, result := r.ReserveControlSignal(context.Background(), impossible); !errors.Is(result.Err, failure.HandoffNotPending) {
		t.Fatalf("ready in agent-owned phase err=%v", result.Err)
	}
}
