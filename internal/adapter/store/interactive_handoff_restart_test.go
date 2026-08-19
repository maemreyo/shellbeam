package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestHumanWriteAuthorityProvenanceIsMonotonicAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, binding, _, _ := h2HandoffFixture(t, root, "provenance")
	got, err := r.LoadInputAuthorityProvenance(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || got != receipt.InputAuthorityAgentOnly {
		t.Fatalf("initial provenance=%q err=%v", got, err)
	}
	if result := r.MarkHumanWriteAuthorityGranted(context.Background(), operation.SessionID(binding.SessionID)); result.Err != nil || result.Durability == app.NoDurableChange {
		t.Fatalf("mark result=%#v", result)
	}
	if result := r.MarkHumanWriteAuthorityGranted(context.Background(), operation.SessionID(binding.SessionID)); result.Err != nil {
		t.Fatalf("idempotent mark=%#v", result)
	}
	reopened := delegatedRepository(t, root, 64)
	got, err = reopened.LoadInputAuthorityProvenance(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || got != receipt.InputAuthorityHumanWriteGranted {
		t.Fatalf("restarted provenance=%q err=%v", got, err)
	}
}

func TestHandoffCrashAfterEpochRotationRecoversBothIngressFenced(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, binding, req, initial := h2HandoffFixture(t, root, "fault")
	createWrites := 0
	r.writer.fail = func(point string) error {
		if point == "create.write" {
			createWrites++
			if createWrites == 2 {
				return errors.New("inject handoff record failure after binding rotation")
			}
		}
		return nil
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err == nil {
		t.Fatal("faulted reserve unexpectedly succeeded")
	}
	rotated, err := r.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || rotated.AuthorityEpoch != initial.AuthorityEpoch {
		t.Fatalf("binding was not durably rotated before injected failure: %#v err=%v", rotated, err)
	}

	reopened := delegatedRepository(t, root, 64)
	candidates, err := reopened.ListHandoffRecoveryCandidates(context.Background())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("recovery candidates=%#v err=%v", candidates, err)
	}
	candidate := candidates[0]
	if candidate.HandoffID != req.HandoffID || candidate.AgentIngress != "fenced" || candidate.HumanIngress != "fenced" {
		t.Fatalf("recovery candidate not fail-closed: %#v", candidate)
	}
	if _, _, result := reopened.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatalf("retry did not recover transaction: %#v", result)
	}
	if candidates, err := reopened.ListHandoffRecoveryCandidates(context.Background()); err != nil || len(candidates) != 1 {
		// Live handoffs remain recovery candidates; what must disappear is the partial transaction marker.
		t.Fatalf("post-retry recovery candidates=%#v err=%v", candidates, err)
	}
}

func TestAdvanceHandoffCrashAfterBindingChangeRecoversAndClearsTransaction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, _, req, initial := h2HandoffFixture(t, root, "advance-fault")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	human := initial
	human.Phase = "human_owned"
	human.ProviderOwner = "human"
	human.AgentIngress = "fenced"
	human.HumanIngress = "writable"
	human.HumanClient = &handoff.HumanClientRef{Ref: "human-client-advance-fault"}
	human.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), human); result.Err != nil {
		t.Fatal(result.Err)
	}

	agent := human
	agent.Phase = handoff.PhaseAgentOwned
	agent.AuthorityEpoch++
	agent.DesiredOwner = "agent"
	agent.ProviderOwner = "agent"
	agent.AgentIngress = "writable"
	agent.HumanIngress = "fenced"
	agent.HumanClient = nil
	agent.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryHumanAttested, Established: true}
	replaceWrites := 0
	r.writer.fail = func(point string) error {
		if point == "replace.write" {
			replaceWrites++
			if replaceWrites == 2 {
				return errors.New("inject handoff state failure after binding advance")
			}
		}
		return nil
	}
	if result := r.AdvanceHandoff(context.Background(), agent); result.Err == nil {
		t.Fatal("faulted advance unexpectedly succeeded")
	}

	reopened := delegatedRepository(t, root, 64)
	if result := reopened.AdvanceHandoff(context.Background(), agent); result.Err != nil {
		t.Fatalf("retry advance failed: %#v", result)
	}
	_, loaded, err := reopened.LoadHandoff(context.Background(), req.HandoffID)
	if err != nil || loaded != agent {
		t.Fatalf("loaded after retry=%#v err=%v", loaded, err)
	}
	if _, err := os.Stat(reopened.interactiveHandoffTransactionPath(req.HandoffID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale transaction marker remains: %v", err)
	}
	candidates, err := reopened.ListHandoffRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.HandoffID == req.HandoffID {
			t.Fatalf("reclaimed agent-owned handoff remained recovery candidate: %#v", candidate)
		}
	}
}

func TestLoadHandoffRejectsUnsafeIdentityBeforePathResolution(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 64)
	if _, _, err := r.LoadHandoff(context.Background(), "../escape"); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("unsafe handoff id err=%v", err)
	}
}

func TestHumanWriteAuthorityProvenanceRejectsUnsafeSessionIdentity(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 64)
	unsafe := operation.SessionID("../escape")
	if result := r.MarkHumanWriteAuthorityGranted(context.Background(), unsafe); !errors.Is(result.Err, failure.InvalidInput) {
		t.Fatalf("unsafe provenance mark err=%v", result.Err)
	}
	if _, err := r.LoadInputAuthorityProvenance(context.Background(), unsafe); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("unsafe provenance load err=%v", err)
	}
}
