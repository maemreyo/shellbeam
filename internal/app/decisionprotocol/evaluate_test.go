package decisionprotocol

import (
	"context"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func task7Policy(requirements ...core.DecisionRequirement) core.PolicyContent {
	return core.PolicyContent{PolicyID: "task7", EpisodeKinds: []core.EpisodeKind{core.EpisodeDiagnosis}, Requirements: requirements, OverridePolicy: core.OverridePolicy{Allowed: false}}
}

func TestRequirementCandidateChallengeCountsDistinctLineageRoots(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}
	facts := core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a2", Candidates: []core.CandidateEvaluationFact{
		{CandidateID: "a", LineageRoot: "a"}, {CandidateID: "a2", LineageRoot: "a"}, {CandidateID: "b", LineageRoot: "b"},
	}}
	got := EvaluateRequirements(task7Policy(req), facts)
	if len(got.RequirementEvaluations) != 1 || got.RequirementEvaluations[0].Status != core.RequirementSatisfied {
		t.Fatalf("evaluation=%#v", got)
	}
	facts.Candidates = facts.Candidates[:2]
	got = EvaluateRequirements(task7Policy(req), facts)
	if got.RequirementEvaluations[0].Status != core.RequirementUnsatisfied {
		t.Fatalf("revision counted as challenge: %#v", got)
	}
}

func TestRequirementPredictionEvaluationCountsOnlySelectedDeterminatePredictions(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "prediction", Kind: core.RequirementPredictionEvaluation, PredictionEvaluation: &core.PredictionEvaluationRequirement{MinimumEvaluatedPredictions: 1, Roles: []core.PredictionRole{core.PredictionRequired}}}
	facts := core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a", Predictions: []core.PredictionEvaluationFact{
		{PredictionID: "pa", CandidateID: "a", Role: core.PredictionRequired, Sealed: true, Linked: true, Status: core.PredictionMatch},
		{PredictionID: "pb", CandidateID: "b", Role: core.PredictionRequired, Sealed: true, Linked: true, Status: core.PredictionMismatch},
		{PredictionID: "pc", CandidateID: "a", Role: core.PredictionRequired, Sealed: true, Linked: true, Status: core.PredictionIndeterminate},
	}}
	got := EvaluateRequirements(task7Policy(req), facts)
	if got.RequirementEvaluations[0].Status != core.RequirementSatisfied {
		t.Fatalf("evaluation=%#v", got)
	}
	facts.Predictions[0].Status = core.PredictionNotEvaluated
	got = EvaluateRequirements(task7Policy(req), facts)
	if got.RequirementEvaluations[0].Status != core.RequirementUnsatisfied {
		t.Fatalf("nondeterminate prediction counted: %#v", got)
	}
}

func TestRequirementDiscriminationAttemptedAndRealizedRemainDistinct(t *testing.T) {
	attempted := core.DecisionRequirement{RequirementID: "discriminate", Kind: core.RequirementDiscrimination, Discrimination: &core.DiscriminationRequirement{MinimumQualifyingExperiments: 1, RequiredOutcome: core.DiscriminationAttempted}}
	realized := attempted
	realized.Discrimination = &core.DiscriminationRequirement{MinimumQualifyingExperiments: 1, RequiredOutcome: core.DiscriminationRealized}
	facts := core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a", Experiments: []core.DiscriminationEvaluationFact{{ExperimentID: "exp-1", CandidateID: "a", Potential: true, Linked: true, ObservationSettled: true, Closed: true, Realized: core.DiscriminationResultNoPartition}}}
	if got := EvaluateRequirements(task7Policy(attempted), facts); got.RequirementEvaluations[0].Status != core.RequirementSatisfied {
		t.Fatalf("attempted=%#v", got)
	}
	if got := EvaluateRequirements(task7Policy(realized), facts); got.RequirementEvaluations[0].Status != core.RequirementUnsatisfied {
		t.Fatalf("nonpartitioning realized=%#v", got)
	}
	facts.Experiments[0].Realized = core.DiscriminationResultUnavailable
	if got := EvaluateRequirements(task7Policy(realized), facts); got.RequirementEvaluations[0].Status != core.RequirementIndeterminate {
		t.Fatalf("unavailable realized=%#v", got)
	}
	facts.Experiments[0].Realized = core.DiscriminationResultPartitioned
	if got := EvaluateRequirements(task7Policy(realized), facts); got.RequirementEvaluations[0].Status != core.RequirementSatisfied {
		t.Fatalf("partitioned realized=%#v", got)
	}
	facts.Experiments[0].Aborted, facts.Experiments[0].Closed = true, false
	if got := EvaluateRequirements(task7Policy(attempted), facts); got.RequirementEvaluations[0].Status == core.RequirementSatisfied {
		t.Fatalf("aborted experiment satisfied attempted: %#v", got)
	}
}

func TestRequirementVerifierAssessmentStartsUnsatisfied(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "verifier", Kind: core.RequirementVerifierAssessment, VerifierAssessment: &core.VerifierAssessmentRequirement{MinimumSupportingAssessments: 1}}
	got := EvaluateRequirements(task7Policy(req), core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a"})
	if got.RequirementEvaluations[0].Status != core.RequirementUnsatisfied {
		t.Fatalf("zero verifier assessments=%#v", got)
	}
}

func TestRequiredPredictionMismatchCreatesImplicitBlockerAndGate(t *testing.T) {
	facts := core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a", Predictions: []core.PredictionEvaluationFact{{PredictionID: "p-required", CandidateID: "a", Role: core.PredictionRequired, Sealed: true, Linked: true, Status: core.PredictionMismatch}}}
	got := EvaluateRequirements(task7Policy(), facts)
	if got.Gate != core.GateBlocked || len(got.CandidateContractBlockers) != 1 || got.CandidateContractBlockers[0].PredictionID != "p-required" {
		t.Fatalf("evaluation=%#v", got)
	}
	facts.Predictions[0].Status = core.PredictionIndeterminate
	got = EvaluateRequirements(task7Policy(), facts)
	if got.Gate != core.GateClear || len(got.CandidateContractBlockers) != 0 {
		t.Fatalf("indeterminate required prediction blocked: %#v", got)
	}
}

func TestBudgetAdmissionIsSeparateFromProtocolGate(t *testing.T) {
	one := uint64(1)
	budget := core.DecisionBudget{MaxExperimentsStarted: &one, MaxLinkedOperations: &one, MaxMachineWallMS: &one}
	got := EvaluateBudget(budget, core.BudgetUsage{ExperimentsStarted: 1, LinkedOperations: 1, MachineWallMS: 2, MachineWallQuality: core.MachineWallObservedNotHard})
	if got.MayStartExperiment || got.MayLinkOperation || !got.ExperimentsExhausted || !got.MachineWallExhausted || got.MachineWallQuality != core.MachineWallObservedNotHard {
		t.Fatalf("budget=%#v", got)
	}
}

func task7ServiceWithPolicy(t *testing.T, content core.PolicyContent) (*Service, *fakePolicyStore, *fakeEpisodeLedger, string) {
	t.Helper()
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakePolicyStore{}
	policy.currentSnapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: content}
	policy.snapshot, policy.put = policy.currentSnapshot, true
	policy.currentActivation = core.PolicyActivation{ActivationID: "act-task7", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ActivationGeneration: "gen_" + strings.Repeat("b", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	policy.currentOK = true
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-task7", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	return svc, policy, ledger, snap.Generation
}

func TestServiceEvaluateUsesEpisodeBoundPolicySnapshot(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}
	content := task7Policy(req)
	svc, policy, _, _ := task7ServiceWithPolicy(t, content)
	for _, candidate := range []core.Candidate{{CandidateID: "a", EpisodeID: "ep-task7", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}, {CandidateID: "b", EpisodeID: "ep-task7", SemanticClaim: "B", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(21, 0).UTC()}} {
		if _, err := svc.CreateCandidate(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
	}
	// Current effective policy may move; the existing episode must still evaluate against its bound snapshot.
	policy.currentSnapshot.Content.Requirements = nil
	got, err := svc.Evaluate(context.Background(), "ep-task7", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Gate != core.GateClear || len(got.RequirementEvaluations) != 1 || got.RequirementEvaluations[0].Kind != core.RequirementCandidateChallenge {
		t.Fatalf("evaluation=%#v", got)
	}
}

func TestServiceProjectPopulatesBudgetAndAllowedTransitions(t *testing.T) {
	one := uint64(1)
	content := task7Policy()
	content.Budget = core.DecisionBudget{MaxExperimentsStarted: &one}
	svc, _, ledger, _ := task7ServiceWithPolicy(t, content)
	candidate := core.Candidate{CandidateID: "a", EpisodeID: "ep-task7", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}
	if _, err := svc.CreateCandidate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	experiment := core.Experiment{SchemaVersion: 1, ExperimentID: "exp-budget", EpisodeID: "ep-task7", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(30, 0).UTC()}
	if _, err := ledger.append(core.RecordExperiment, experiment); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Project(context.Background(), "ep-task7", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Protocol.Gate != core.GateClear || got.Budget.MayStartExperiment || got.ProjectionDigest == "" || got.AuditDigest == "" {
		t.Fatalf("projection=%#v", got)
	}
	if containsString(got.AllowedProtocolTransitions, "decision.experiment.define") || !containsString(got.AllowedProtocolTransitions, "decision.close_unresolved") || !containsString(got.AllowedProtocolTransitions, "decision.selection.commit") {
		t.Fatalf("transitions=%v", got.AllowedProtocolTransitions)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
