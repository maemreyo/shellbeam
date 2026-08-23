//go:build linux || darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDecisionProtocolNativeRestartReplaysCanonicalBoundaries(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir, workspace := newDecisionRestartWorkspace(t)
	content := decisioncore.PolicyContent{PolicyID: "decision-restart-policy", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}}
	activationReq := ipcadapter.DecisionRequestV1{ActivationID: "decision-restart-activation", ProposalGeneration: "gen_" + strings.Repeat("a", 64), ExpectedPreviousPolicyDigest: "absent", ActorRef: "restart-actor"}
	policyDigest := restartActivationAndCandidates(t, binary, stateDir, runtimeDir, content, &activationReq)
	seal := restartSealExperiment(t, binary, stateDir, runtimeDir)
	closedDigest := restartExecuteAndCloseExperiment(t, binary, stateDir, runtimeDir, workspace, seal)
	restartCommitAndReplaySelection(t, binary, stateDir, runtimeDir, policyDigest, closedDigest)
}

func restartActivationAndCandidates(t *testing.T, binary, stateDir, runtimeDir string, content decisioncore.PolicyContent, activationReq *ipcadapter.DecisionRequestV1) string {
	t.Helper()
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	snapshot := callDecisionNative(t, first, "decision.policy.snapshot", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}})
	if snapshot.Policy == nil {
		t.Fatal("snapshot omitted")
	}
	activationReq.PolicyDigest = snapshot.Policy.PolicyDigest
	activation := callDecisionNative(t, first, "decision.policy.activate", *activationReq)
	if activation.Activation == nil {
		t.Fatal("activation omitted")
	}
	first.hardKill(t)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	replayed := callDecisionNative(t, second, "decision.policy.activate", *activationReq)
	if replayed.Activation == nil || replayed.Activation.ActivationID != activation.Activation.ActivationID || !replayed.Activation.ActivatedAt.Equal(activation.Activation.ActivatedAt) {
		t.Fatalf("activation replay first=%#v replay=%#v", activation.Activation, replayed.Activation)
	}
	callDecisionNative(t, second, "decision.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "restart-actor"})
	parent := &ipcadapter.DecisionCandidateInputV1{CandidateID: "candidate-parent", SemanticClaim: "parent"}
	child := &ipcadapter.DecisionCandidateInputV1{CandidateID: "candidate-child", SemanticClaim: "child"}
	other := &ipcadapter.DecisionCandidateInputV1{CandidateID: "candidate-other", SemanticClaim: "other"}
	callDecisionNative(t, second, "decision.candidate.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", Candidate: parent, ActorRef: "restart-actor"})
	callDecisionNative(t, second, "decision.candidate.revise", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", ParentCandidateID: "candidate-parent", Candidate: child, ActorRef: "restart-actor"})
	callDecisionNative(t, second, "decision.candidate.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", Candidate: other, ActorRef: "restart-actor"})
	second.hardKill(t)
	return snapshot.Policy.PolicyDigest
}

func restartSealExperiment(t *testing.T, binary, stateDir, runtimeDir string) decisioncore.ExperimentSeal {
	t.Helper()
	third := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	callDecisionNative(t, third, "decision.candidate.revise", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", ParentCandidateID: "candidate-parent", Candidate: &ipcadapter.DecisionCandidateInputV1{CandidateID: "candidate-child", SemanticClaim: "child"}, ActorRef: "restart-actor"})
	assertDecisionRestartRevision(t, inspectDecisionNative(t, third, "episode-restart", "candidate-child"))
	callDecisionNative(t, third, "decision.experiment.define", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", ExperimentID: "experiment-restart", ActorRef: "restart-actor"})
	for _, prediction := range []ipcadapter.DecisionPredictionInputV1{
		operationOutcomePrediction("prediction-child", "candidate-child", decisioncore.OperationSuccess),
		operationOutcomePrediction("prediction-other", "candidate-other", decisioncore.OperationFailure),
	} {
		callDecisionNative(t, third, "decision.prediction.bind", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", ExperimentID: "experiment-restart", Prediction: &prediction})
	}
	sealed := callDecisionNative(t, third, "decision.experiment.seal", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-restart", ActorRef: "restart-actor"})
	if sealed.Seal == nil {
		t.Fatal("seal omitted")
	}
	seal := *sealed.Seal
	third.hardKill(t)
	return seal
}

func restartExecuteAndCloseExperiment(t *testing.T, binary, stateDir, runtimeDir string, workspace workspacecore.Workspace, seal decisioncore.ExperimentSeal) string {
	t.Helper()
	fourth := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	replayed := callDecisionNative(t, fourth, "decision.experiment.seal", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-restart", ActorRef: "restart-actor"})
	if replayed.Seal == nil || replayed.Seal.SealedPredictionDigest != seal.SealedPredictionDigest || !replayed.Seal.SealedAt.Equal(seal.SealedAt) {
		t.Fatalf("seal replay first=%#v replay=%#v", seal, replayed.Seal)
	}
	started := callB1NativeDaemon(t, fourth, ipcadapter.RequestV2{Action: "start", OperationID: "operation-restart", WorkspaceID: string(workspace.ID), CWD: ".", Command: "true", ExperimentID: "experiment-restart", YieldMS: 20, MaxOutputBytes: 4096})
	if started.Result == nil {
		t.Fatal("linked start omitted result")
	}
	waitB1NativeTerminal(t, fourth.client, started.Result.Operation.SessionID)
	closed := callDecisionNative(t, fourth, "decision.experiment.close", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-restart", ActorRef: "restart-actor"})
	if closed.Projection == nil {
		t.Fatal("close omitted projection")
	}
	closedDigest := closed.Projection.ProjectionDigest
	fourth.hardKill(t)
	removeDecisionRestartDerivedObservations(t, stateDir)
	return closedDigest
}

func removeDecisionRestartDerivedObservations(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(stateDir, "observations", "events")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateDir, "observations", "projection-state.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func restartCommitAndReplaySelection(t *testing.T, binary, stateDir, runtimeDir, policyDigest, closedDigest string) {
	t.Helper()
	fifth := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	replayedClose := callDecisionNative(t, fifth, "decision.experiment.close", ipcadapter.DecisionRequestV1{ExperimentID: "experiment-restart", ActorRef: "restart-actor"})
	if replayedClose.Projection == nil || replayedClose.Projection.ProjectionDigest != closedDigest {
		t.Fatalf("observation replay before=%q after=%#v", closedDigest, replayedClose.Projection)
	}
	precommit := inspectDecisionNative(t, fifth, "episode-restart", "candidate-child")
	if precommit.Protocol.Gate != decisioncore.GateClear {
		t.Fatalf("precommit gate=%s projection=%#v", precommit.Protocol.Gate, precommit)
	}
	commitReq := ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart", CandidateID: "candidate-child", ActorRef: "restart-actor", ExpectedPolicyDigest: policyDigest, ExpectedProjectionDigest: precommit.ProjectionDigest, IdempotencyKey: "restart-selection"}
	commit := callDecisionNative(t, fifth, "decision.selection.commit", commitReq)
	if commit.Selection == nil {
		t.Fatal("selection omitted")
	}
	fifth.hardKill(t)

	sixth := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer sixth.hardKill(t)
	replayedCommit := callDecisionNative(t, sixth, "decision.selection.commit", commitReq)
	if replayedCommit.Selection == nil || replayedCommit.Selection.CommitID != commit.Selection.CommitID || !replayedCommit.Selection.CommittedAt.Equal(commit.Selection.CommittedAt) {
		t.Fatalf("commit replay first=%#v replay=%#v", commit.Selection, replayedCommit.Selection)
	}
	if final := inspectDecisionNative(t, sixth, "episode-restart", "candidate-child"); final.EpisodeState != decisioncore.EpisodeCommitted {
		t.Fatalf("final=%#v", final)
	}
}

func TestDecisionProtocolNativeRestartPreservesOverrideAuthorizationCommit(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir, workspace := newDecisionRestartWorkspace(t)
	class := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
	content := decisioncore.PolicyContent{PolicyID: "decision-restart-override", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, Requirements: []decisioncore.DecisionRequirement{{RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge, CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}}, OverridePolicy: decisioncore.OverridePolicy{Allowed: true, RequiredAuthorityClass: &class}}
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	policy := callDecisionNative(t, first, "decision.policy.snapshot", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}})
	callDecisionNative(t, first, "decision.policy.activate", ipcadapter.DecisionRequestV1{ActivationID: "restart-override-activation", PolicyDigest: policy.Policy.PolicyDigest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ExpectedPreviousPolicyDigest: "absent", ActorRef: "actor"})
	createDecisionEpisodeAndCandidate(t, first, "episode-restart-override", "candidate-restart-override")
	projection := inspectDecisionNative(t, first, "episode-restart-override", "candidate-restart-override")
	scope := decisioncore.AuthorityScope{RepositoryID: string(workspace.RepositoryID), EpisodeID: "episode-restart-override", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}
	authority := callDecisionNative(t, first, "decision.authority.materialize", ipcadapter.DecisionRequestV1{AuthorityRequest: &ipcadapter.DecisionAuthorityMaterializeInputV1{RequiredAuthorityClass: class, RequiredScope: scope}})
	override := callDecisionNative(t, first, "decision.override.create", ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart-override", CandidateID: "candidate-restart-override", ExpectedPolicyDigest: policy.Policy.PolicyDigest, ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: projection.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: authority.Attestation.AttestationID, Reason: "restart override"})
	commitReq := ipcadapter.DecisionRequestV1{EpisodeID: "episode-restart-override", CandidateID: "candidate-restart-override", ActorRef: "actor", ExpectedPolicyDigest: policy.Policy.PolicyDigest, ExpectedProjectionDigest: projection.ProjectionDigest, OverrideRef: override.Override.OverrideID, IdempotencyKey: "restart-override-commit"}
	commit := callDecisionNative(t, first, "decision.selection.commit", commitReq)
	if commit.Selection == nil || commit.Selection.OverrideAuthorization == nil {
		t.Fatalf("commit=%#v", commit.Selection)
	}
	first.hardKill(t)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	replay := callDecisionNative(t, second, "decision.selection.commit", commitReq)
	if replay.Selection == nil || replay.Selection.CommitID != commit.Selection.CommitID || !reflect.DeepEqual(replay.Selection.OverrideAuthorization, commit.Selection.OverrideAuthorization) {
		t.Fatalf("override replay first=%#v replay=%#v", commit.Selection, replay.Selection)
	}
}

func newDecisionRestartWorkspace(t *testing.T) (stateDir, runtimeDir string, workspace workspacecore.Workspace) {
	t.Helper()
	stateDir, runtimeDir = b1NativeDirs(t)
	repo := initWorkspaceCLIRepo(t)
	store := openA1Store(t, stateDir)
	service := workspaceapp.New(store, gitadapter.New())
	var err error
	workspace, err = service.Attach(context.Background(), repo, "decision-restart")
	if err != nil {
		t.Fatal(err)
	}
	return stateDir, runtimeDir, workspace
}

func assertDecisionRestartRevision(t *testing.T, projection decisioncore.DecisionProjection) {
	t.Helper()
	states := map[decisioncore.CandidateID]decisioncore.CandidateLifecycleState{}
	for _, candidate := range projection.Candidates {
		states[candidate.CandidateID] = candidate.State
	}
	if states["candidate-parent"] != decisioncore.CandidateSuperseded || states["candidate-child"] != decisioncore.CandidateActive {
		t.Fatalf("candidate states=%#v", states)
	}
}
