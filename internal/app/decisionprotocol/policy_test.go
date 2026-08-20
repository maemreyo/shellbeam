package decisionprotocol

import (
	"context"
	"errors"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type fakePolicyStore struct {
	snapshot                 core.PolicySnapshot
	put                      bool
	activationCommit         core.PolicyActivationCommit
	currentSnapshot          core.PolicySnapshot
	currentActivation        core.PolicyActivation
	currentOK                bool
	existingActivationCommit core.PolicyActivationCommit
	existingActivation       bool
}

func (f *fakePolicyStore) PutPolicySnapshot(_ context.Context, s core.PolicySnapshot) (bool, error) {
	f.snapshot = s
	f.put = true
	return true, nil
}
func (f *fakePolicyStore) LoadPolicySnapshot(context.Context, string, string) (core.PolicySnapshot, bool, error) {
	return f.snapshot, f.put, nil
}
func (f *fakePolicyStore) FindPolicyActivationCommit(context.Context, string, string) (core.PolicyActivationCommit, bool, error) {
	return f.existingActivationCommit, f.existingActivation, nil
}
func (f *fakePolicyStore) ActivatePolicyCAS(_ context.Context, commit core.PolicyActivationCommit) (core.PolicyActivationWriteResult, error) {
	f.activationCommit = commit
	record := core.PolicyActivation{ActivationID: commit.Intent.ActivationID, RepositoryID: commit.Intent.RepositoryID, PolicyDigest: commit.Intent.ProposedPolicyDigest, ProposalGeneration: commit.Intent.ProposalGeneration, ActivationGeneration: commit.ActivationGeneration, Authority: commit.Intent.Authority, ActorRef: commit.Intent.ActorRef}
	return core.PolicyActivationWriteResult{Record: record, Created: true, Effective: true}, nil
}
func (f *fakePolicyStore) CurrentEffectivePolicy(context.Context, string, core.EpisodeKind) (core.PolicySnapshot, core.PolicyActivation, bool, error) {
	return f.currentSnapshot, f.currentActivation, f.currentOK, nil
}

func TestPutDecisionPolicySnapshotDerivesRepositoryAndDigestFromContent(t *testing.T) {
	store := &fakePolicyStore{}
	svc := NewService(store, nil)
	content := core.PolicyContent{PolicyID: "p1", EpisodeKinds: []core.EpisodeKind{core.EpisodeDiagnosis}, OverridePolicy: core.OverridePolicy{Allowed: false}}
	got, err := svc.PutPolicySnapshot(context.Background(), PutPolicySnapshotRequest{RepositoryID: "repo-a", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if got.RepositoryID != "repo-a" || got.PolicyDigest == "" || !store.put || store.snapshot.PolicyDigest != got.PolicyDigest {
		t.Fatalf("got=%#v stored=%#v", got, store.snapshot)
	}
}

type fakeActivationGenerationSource struct{ generation string }

func (f fakeActivationGenerationSource) CurrentActivationGeneration(context.Context, string) (string, error) {
	return f.generation, nil
}

func TestActivateDecisionPolicyUsesServerGenerationAndExplicitPreviousDigest(t *testing.T) {
	store := &fakePolicyStore{}
	generation := "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	svc := NewService(store, fakeActivationGenerationSource{generation: generation})
	policyDigest := "pol_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	proposalGeneration := "gen_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	got, err := svc.ActivatePolicy(context.Background(), ActivatePolicyRequest{RepositoryID: "repo-a", ActivationID: "act-1", PolicyDigest: policyDigest, ProposalGeneration: proposalGeneration, ExpectedPreviousPolicyDigest: "absent", ActorRef: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ActivationGeneration != generation || store.activationCommit.ActivationGeneration != generation {
		t.Fatalf("generation got=%q commit=%q", got.ActivationGeneration, store.activationCommit.ActivationGeneration)
	}
	if store.activationCommit.Intent.PreviousEffectivePolicyDigest != "absent" || store.activationCommit.Intent.Authority != core.AuthorityExplicitCaller {
		t.Fatalf("intent=%#v", store.activationCommit.Intent)
	}
}

func TestActivateDecisionPolicyRejectsEmptyPreviousDigest(t *testing.T) {
	store := &fakePolicyStore{}
	svc := NewService(store, fakeActivationGenerationSource{generation: "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	_, err := svc.ActivatePolicy(context.Background(), ActivatePolicyRequest{RepositoryID: "repo-a", ActivationID: "act-1", PolicyDigest: "pol_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ProposalGeneration: "gen_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ExpectedPreviousPolicyDigest: "", ActorRef: "actor"})
	if err == nil {
		t.Fatal("empty previous digest accepted")
	}
}

type failingActivationGenerationSource struct{ calls int }

func (f *failingActivationGenerationSource) CurrentActivationGeneration(context.Context, string) (string, error) {
	f.calls++
	return "", errors.New("fresh generation must not be consulted on durable replay")
}

func TestActivateDecisionPolicyDurableReplayReusesFrozenActivationGenerationBeforeFreshObservation(t *testing.T) {
	policyDigest := "pol_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	proposalGeneration := "gen_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	frozenGeneration := "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	intent := core.PolicyActivationIntent{ActivationID: "act-replay", RepositoryID: "repo-a", PreviousEffectivePolicyDigest: "absent", ProposedPolicyDigest: policyDigest, ProposalGeneration: proposalGeneration, Authority: core.AuthorityExplicitCaller, ActorRef: "actor"}
	store := &fakePolicyStore{existingActivation: true, existingActivationCommit: core.PolicyActivationCommit{Intent: intent, ActivationGeneration: frozenGeneration}}
	generation := &failingActivationGenerationSource{}
	svc := NewService(store, generation)
	got, err := svc.ActivatePolicy(context.Background(), ActivatePolicyRequest{RepositoryID: "repo-a", ActivationID: "act-replay", PolicyDigest: policyDigest, ProposalGeneration: proposalGeneration, ExpectedPreviousPolicyDigest: "absent", ActorRef: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	if generation.calls != 0 {
		t.Fatalf("fresh generation consulted %d times during durable replay", generation.calls)
	}
	if got.ActivationGeneration != frozenGeneration || store.activationCommit.ActivationGeneration != frozenGeneration {
		t.Fatalf("got=%q commit=%q want frozen=%q", got.ActivationGeneration, store.activationCommit.ActivationGeneration, frozenGeneration)
	}
}
