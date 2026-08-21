//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDecisionProtocolNativeNormalSequence(t *testing.T) {
	stateDir, runtimeDir := b1NativeDirs(t)
	repo := initWorkspaceCLIRepo(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	workspace, err := workspaceService.Attach(context.Background(), repo, "decision-normal")
	if err != nil {
		t.Fatal(err)
	}
	daemon := &decisionAcceptanceDaemon{client: runA1Daemon(t, stateDir, runtimeDir)}

	assertDecisionNativeCapability(t, daemon)
	policyDigest := activateDecisionNormalPolicy(t, daemon, workspace)
	prepareDecisionNormalExperiment(t, daemon)
	executeAndCommitDecisionNormal(t, daemon, workspace, stateDir, policyDigest)
}

func assertDecisionNativeCapability(t *testing.T, daemon decisionDaemonClient) {
	t.Helper()
	server := callDecisionDaemon(t, daemon, ipcadapter.RequestV2{Action: "inspect.server"})
	if server.Server == nil || server.Server.DecisionProtocol == nil {
		t.Fatalf("decision protocol capability unavailable: %#v", server.Server)
	}
}

func activateDecisionNormalPolicy(t *testing.T, daemon decisionDaemonClient, workspace workspace.Workspace) string {
	t.Helper()
	policyContent := decisioncore.PolicyContent{PolicyID: "decision-normal-policy", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}}
	snapshot := callDecisionNative(t, daemon, "decision.policy.snapshot", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: policyContent}})
	if snapshot.Policy == nil || snapshot.Policy.RepositoryID != string(workspace.RepositoryID) {
		t.Fatalf("policy snapshot=%#v workspace=%#v", snapshot.Policy, workspace)
	}
	activation := callDecisionNative(t, daemon, "decision.policy.activate", ipcadapter.DecisionRequestV1{
		ActivationID: "decision-normal-activation", PolicyDigest: snapshot.Policy.PolicyDigest,
		ProposalGeneration: "gen_" + strings.Repeat("a", 64), ExpectedPreviousPolicyDigest: "absent", ActorRef: "acceptance-actor",
	})
	if activation.Activation == nil || activation.Activation.PolicyDigest != snapshot.Policy.PolicyDigest {
		t.Fatalf("activation=%#v", activation.Activation)
	}
	return snapshot.Policy.PolicyDigest
}

func prepareDecisionNormalExperiment(t *testing.T, daemon decisionDaemonClient) {
	t.Helper()
	created := callDecisionNative(t, daemon, "decision.create", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "acceptance-actor"})
	if created.Projection == nil || created.Projection.EpisodeID != "decision-normal-episode" {
		t.Fatalf("created projection=%#v", created.Projection)
	}
	for _, candidate := range []ipcadapter.DecisionCandidateInputV1{{CandidateID: "candidate-a", SemanticClaim: "operation fails"}, {CandidateID: "candidate-b", SemanticClaim: "operation succeeds"}} {
		callDecisionNative(t, daemon, "decision.candidate.create", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", Candidate: &candidate, ActorRef: "acceptance-actor"})
	}
	callDecisionNative(t, daemon, "decision.experiment.define", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", ExperimentID: "experiment-normal", ActorRef: "acceptance-actor"})
	for _, prediction := range []ipcadapter.DecisionPredictionInputV1{
		operationOutcomePrediction("prediction-a", "candidate-a", decisioncore.OperationFailure),
		operationOutcomePrediction("prediction-b", "candidate-b", decisioncore.OperationSuccess),
	} {
		callDecisionNative(t, daemon, "decision.prediction.bind", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", ExperimentID: "experiment-normal", Prediction: &prediction})
	}
	sealed := callDecisionNative(t, daemon, "decision.experiment.seal", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-normal", ActorRef: "acceptance-actor"})
	if sealed.Seal == nil || len(sealed.Seal.PotentialDiscriminationPairs) == 0 {
		t.Fatalf("seal=%#v", sealed.Seal)
	}
}

func executeAndCommitDecisionNormal(t *testing.T, daemon decisionDaemonClient, workspace workspace.Workspace, stateDir, policyDigest string) {
	t.Helper()
	started := callDecisionDaemon(t, daemon, ipcadapter.RequestV2{Action: "start", OperationID: "decision-normal-operation", WorkspaceID: string(workspace.ID), CWD: ".", Command: "true", ExperimentID: "experiment-normal", YieldMS: 20, MaxOutputBytes: 4096})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	terminal := waitB1NativeTerminal(t, daemon.decisionClient(), started.Result.Operation.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.Outcome != session.Success {
		t.Fatalf("terminal=%#v", terminal)
	}
	assertDecisionNormalAdmissionTruth(t, stateDir, terminal.Receipt)
	closed := callDecisionNative(t, daemon, "decision.experiment.close", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-normal", ActorRef: "acceptance-actor"})
	if closed.Projection == nil {
		t.Fatal("experiment close omitted projection")
	}
	evaluated := callDecisionNative(t, daemon, "decision.evaluate", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", CandidateID: "candidate-b"})
	if evaluated.Evaluation == nil || evaluated.Evaluation.Gate != decisioncore.GateClear {
		t.Fatalf("evaluation=%#v", evaluated.Evaluation)
	}
	inspect := callDecisionNative(t, daemon, "decision.inspect", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", CandidateID: "candidate-b"})
	if inspect.Projection == nil || inspect.Projection.Protocol.Gate != decisioncore.GateClear {
		t.Fatalf("inspect projection=%#v", inspect.Projection)
	}
	callDecisionNative(t, daemon, "decision.selection.propose", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", CandidateID: "candidate-b", ActorRef: "acceptance-actor", Reason: "prediction matched"})
	committed := callDecisionNative(t, daemon, "decision.selection.commit", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", CandidateID: "candidate-b", ActorRef: "acceptance-actor", ExpectedPolicyDigest: policyDigest, ExpectedProjectionDigest: inspect.Projection.ProjectionDigest, IdempotencyKey: "decision-normal-commit"})
	if committed.Selection == nil || committed.Selection.CandidateID != "candidate-b" {
		t.Fatalf("selection=%#v", committed.Selection)
	}
	final := callDecisionNative(t, daemon, "decision.inspect", ipcadapter.DecisionRequestV1{EpisodeID: "decision-normal-episode", CandidateID: "candidate-b"})
	if final.Projection == nil || final.Projection.EpisodeState != decisioncore.EpisodeCommitted {
		t.Fatalf("final projection=%#v", final.Projection)
	}
}

type decisionDaemonClient interface {
	decisionClient() *ipcadapter.Client
}

type decisionAcceptanceDaemon struct{ client *ipcadapter.Client }

func (d *decisionAcceptanceDaemon) decisionClient() *ipcadapter.Client { return d.client }
func (d *b1NativeDaemon) decisionClient() *ipcadapter.Client           { return d.client }

func callDecisionDaemon(t *testing.T, daemon decisionDaemonClient, req ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	if native, ok := daemon.(*b1NativeDaemon); ok {
		return callB1NativeDaemon(t, native, req)
	}
	req.IPVersion, req.Kind = 2, "request"
	if req.RequestID == "" {
		req.RequestID = req.Action + "-acceptance"
	}
	response, err := daemon.decisionClient().CallV2(context.Background(), req)
	if err != nil || !response.OK {
		t.Fatalf("%s call ok=%v ipc_error=%#v err=%v", req.Action, response.OK, response.Error, err)
	}
	return response
}

func callDecisionNative(t *testing.T, daemon decisionDaemonClient, action string, request ipcadapter.DecisionRequestV1) ipcadapter.DecisionResponseV1 {
	t.Helper()
	response := callDecisionDaemon(t, daemon, ipcadapter.RequestV2{Action: action, Decision: &request})
	if response.Decision == nil {
		t.Fatalf("%s omitted decision response", action)
	}
	return *response.Decision
}

func operationOutcomePrediction(id, candidate string, outcome decisioncore.OperationOutcome) ipcadapter.DecisionPredictionInputV1 {
	return ipcadapter.DecisionPredictionInputV1{
		PredictionID: id, CandidateID: candidate, Role: decisioncore.PredictionRequired,
		Predicate: decisioncore.ObservationPredicate{Kind: decisioncore.PredicateOperationOutcome, OperationOutcome: &decisioncore.OperationOutcomePredicate{ExpectedOutcome: outcome}},
	}
}

func assertDecisionNormalAdmissionTruth(t *testing.T, stateDir string, terminal *receipt.Receipt) {
	t.Helper()
	store := openA1Store(t, stateDir)
	canonical := storeadapter.NewDecisionProtocolStore(store)
	hw, err := canonical.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := canonical.ListEpisodeRecords(context.Background(), "decision-normal-episode", hw)
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	for _, record := range records {
		if record.Kind == decisioncore.RecordExperimentExecutionLink {
			links++
		}
	}
	if links != 1 {
		t.Fatalf("execution links=%d records=%#v", links, records)
	}
	if terminal == nil || !terminal.Spawn.Attempted || !terminal.Spawn.Succeeded {
		t.Fatalf("spawn evidence=%#v", terminal)
	}
}

func TestDecisionProtocolBlockedAndIndeterminateCanCloseUnresolved(t *testing.T) {
	t.Run("blocked candidate challenge", func(t *testing.T) {
		daemon, workspace, policy := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{
			PolicyID:     "decision-blocked-policy",
			EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
			Requirements: []decisioncore.DecisionRequirement{{
				RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge,
				CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2},
			}},
			OverridePolicy: decisioncore.OverridePolicy{Allowed: false},
		})
		_ = workspace
		createDecisionEpisodeAndCandidate(t, daemon, "episode-blocked", "candidate-only")
		projection := inspectDecisionNative(t, daemon, "episode-blocked", "candidate-only")
		if projection.Protocol.Gate != decisioncore.GateBlocked {
			t.Fatalf("gate=%s projection=%#v", projection.Protocol.Gate, projection)
		}
		assertDecisionCommitRejected(t, daemon, "episode-blocked", "candidate-only", policy.PolicyDigest, projection.ProjectionDigest)
		closeDecisionUnresolved(t, daemon, "episode-blocked")
	})

	t.Run("unresolved verifier qualification", func(t *testing.T) {
		daemon, _, policy := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{
			PolicyID:     "decision-indeterminate-policy",
			EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
			Requirements: []decisioncore.DecisionRequirement{{
				RequirementID: "verifier", Kind: decisioncore.RequirementVerifierAssessment,
				VerifierAssessment: &decisioncore.VerifierAssessmentRequirement{MinimumSupportingAssessments: 1, RequiredContextClass: decisioncore.ContextIndependentModel},
			}},
			OverridePolicy: decisioncore.OverridePolicy{Allowed: false},
		})
		createDecisionEpisodeAndCandidate(t, daemon, "episode-indeterminate", "candidate-verifier")
		callDecisionNative(t, daemon, "decision.assessment.record", ipcadapter.DecisionRequestV1{
			EpisodeID: "episode-indeterminate", ActorRef: "acceptance-verifier",
			Assessment: &ipcadapter.DecisionAssessmentInputV1{AssessmentID: "assessment-unqualified", DeclaredContextClass: decisioncore.ContextIndependentModel, PreferredCandidates: []string{"candidate-verifier"}, Rationale: "independent review declaration"},
		})
		projection := inspectDecisionNative(t, daemon, "episode-indeterminate", "candidate-verifier")
		if projection.Protocol.Gate != decisioncore.GateIndeterminate {
			t.Fatalf("gate=%s projection=%#v", projection.Protocol.Gate, projection)
		}
		assertDecisionCommitRejected(t, daemon, "episode-indeterminate", "candidate-verifier", policy.PolicyDigest, projection.ProjectionDigest)
		closeDecisionUnresolved(t, daemon, "episode-indeterminate")
	})
}

func TestDecisionProtocolBudgetExhaustionBlocksFurtherExperimentsButNotClosure(t *testing.T) {
	one := uint64(1)
	daemon, _, _ := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{
		PolicyID:     "decision-budget-policy",
		EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
		Requirements: []decisioncore.DecisionRequirement{{
			RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge,
			CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2},
		}},
		Budget:         decisioncore.DecisionBudget{MaxExperimentsStarted: &one},
		OverridePolicy: decisioncore.OverridePolicy{Allowed: false},
	})
	createDecisionEpisodeAndCandidate(t, daemon, "episode-budget", "candidate-budget")
	callDecisionNative(t, daemon, "decision.experiment.define", ipcadapter.DecisionRequestV1{EpisodeID: "episode-budget", ExperimentID: "experiment-budget-1", ActorRef: "actor"})
	projection := inspectDecisionNative(t, daemon, "episode-budget", "candidate-budget")
	if projection.Budget.MayStartExperiment || !projection.Budget.ExperimentsExhausted || containsDecisionTransition(projection.AllowedProtocolTransitions, "decision.experiment.define") {
		t.Fatalf("budget projection=%#v", projection)
	}
	response := callDecisionRawNative(t, daemon, "decision.experiment.define", ipcadapter.DecisionRequestV1{EpisodeID: "episode-budget", ExperimentID: "experiment-budget-2", ActorRef: "actor"})
	if response.OK {
		t.Fatalf("budget-exhausted experiment admitted: %#v", response.Decision)
	}
	closeDecisionUnresolved(t, daemon, "episode-budget")
}

func startDecisionAcceptanceDaemon(t *testing.T, content decisioncore.PolicyContent) (*decisionAcceptanceDaemon, workspace.Workspace, decisioncore.PolicySnapshot) {
	t.Helper()
	stateDir, runtimeDir := b1NativeDirs(t)
	repo := initWorkspaceCLIRepo(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	ws, err := workspaceService.Attach(context.Background(), repo, content.PolicyID)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &decisionAcceptanceDaemon{client: runA1Daemon(t, stateDir, runtimeDir)}
	snapshot := callDecisionNative(t, daemon, "decision.policy.snapshot", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}})
	if snapshot.Policy == nil {
		t.Fatal("policy snapshot omitted")
	}
	callDecisionNative(t, daemon, "decision.policy.activate", ipcadapter.DecisionRequestV1{
		ActivationID: content.PolicyID + "-activation", PolicyDigest: snapshot.Policy.PolicyDigest,
		ProposalGeneration: "gen_" + strings.Repeat("a", 64), ExpectedPreviousPolicyDigest: "absent", ActorRef: "acceptance-actor",
	})
	return daemon, ws, *snapshot.Policy
}

func createDecisionEpisodeAndCandidate(t *testing.T, daemon decisionDaemonClient, episodeID, candidateID string) {
	t.Helper()
	callDecisionNative(t, daemon, "decision.create", ipcadapter.DecisionRequestV1{EpisodeID: episodeID, EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "acceptance-actor"})
	candidate := &ipcadapter.DecisionCandidateInputV1{CandidateID: candidateID, SemanticClaim: "candidate claim"}
	callDecisionNative(t, daemon, "decision.candidate.create", ipcadapter.DecisionRequestV1{EpisodeID: episodeID, Candidate: candidate, ActorRef: "acceptance-actor"})
}

func inspectDecisionNative(t *testing.T, daemon decisionDaemonClient, episodeID, candidateID string) decisioncore.DecisionProjection {
	t.Helper()
	response := callDecisionNative(t, daemon, "decision.inspect", ipcadapter.DecisionRequestV1{EpisodeID: episodeID, CandidateID: candidateID})
	if response.Projection == nil {
		t.Fatal("decision inspect omitted projection")
	}
	return *response.Projection
}

func assertDecisionCommitRejected(t *testing.T, daemon decisionDaemonClient, episodeID, candidateID, policyDigest, projectionDigest string) {
	t.Helper()
	response := callDecisionRawNative(t, daemon, "decision.selection.commit", ipcadapter.DecisionRequestV1{
		EpisodeID: episodeID, CandidateID: candidateID, ActorRef: "acceptance-actor", ExpectedPolicyDigest: policyDigest,
		ExpectedProjectionDigest: projectionDigest, IdempotencyKey: episodeID + "-blocked-commit",
	})
	if response.OK {
		t.Fatalf("non-clear protocol committed: %#v", response.Decision)
	}
}

func closeDecisionUnresolved(t *testing.T, daemon decisionDaemonClient, episodeID string) {
	t.Helper()
	projection := inspectDecisionNative(t, daemon, episodeID, "")
	dimensions := []string{}
	closed := callDecisionNative(t, daemon, "decision.close_unresolved", ipcadapter.DecisionRequestV1{EpisodeID: episodeID, ActorRef: "acceptance-actor", ExpectedProjectionDigest: projection.ProjectionDigest, Reason: "acceptance unresolved", UnresolvedDimensions: &dimensions})
	if closed.Closure == nil || closed.Closure.Kind != decisioncore.ClosureUnresolved {
		t.Fatalf("closure=%#v", closed.Closure)
	}
	final := inspectDecisionNative(t, daemon, episodeID, "")
	if final.EpisodeState != decisioncore.EpisodeClosedUnresolved {
		t.Fatalf("episode state=%s", final.EpisodeState)
	}
}

func callDecisionRawNative(t *testing.T, daemon decisionDaemonClient, action string, request ipcadapter.DecisionRequestV1) ipcadapter.ResponseV2 {
	t.Helper()
	response, err := daemon.decisionClient().CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: action + "-raw", Action: action, Decision: &request})
	if err != nil {
		t.Fatalf("%s raw call: %v", action, err)
	}
	return response
}

func containsDecisionTransition(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDecisionProtocolProductionProjectionOmitsUnavailableOracleMetrics(t *testing.T) {
	daemon, _, policy := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{PolicyID: "decision-oracle-unavailable", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}})
	callDecisionNative(t, daemon, "decision.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-oracle", EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "oracle-actor"})
	for _, candidate := range []*ipcadapter.DecisionCandidateInputV1{
		{CandidateID: "candidate-selected", SemanticClaim: "selected"},
		{CandidateID: "candidate-losing-unevaluated", SemanticClaim: "not exhaustively oracle evaluated"},
	} {
		callDecisionNative(t, daemon, "decision.candidate.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-oracle", Candidate: candidate, ActorRef: "oracle-actor"})
	}
	projection := inspectDecisionNative(t, daemon, "episode-oracle", "candidate-selected")
	if projection.Protocol.Gate != decisioncore.GateClear {
		t.Fatalf("gate=%s", projection.Protocol.Gate)
	}
	callDecisionNative(t, daemon, "decision.selection.commit", ipcadapter.DecisionRequestV1{EpisodeID: "episode-oracle", CandidateID: "candidate-selected", ActorRef: "oracle-actor", ExpectedPolicyDigest: policy.PolicyDigest, ExpectedProjectionDigest: projection.ProjectionDigest, IdempotencyKey: "oracle-unavailable-commit"})
	final := callDecisionNative(t, daemon, "decision.inspect", ipcadapter.DecisionRequestV1{EpisodeID: "episode-oracle", CandidateID: "candidate-selected"})
	encoded, err := json.Marshal(final)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"oracle_metrics", "pass@n", "pass_at", "candidate_recall", "selection_regret", "recall", "regret"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("production response invented unavailable oracle metric %q: %s", forbidden, encoded)
		}
	}
}
