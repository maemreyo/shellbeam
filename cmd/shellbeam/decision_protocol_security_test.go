//go:build linux || darwin

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	decisionapp "github.com/maemreyo/shellbeam/internal/app/decisionprotocol"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDecisionProtocolNativeExplicitCallerOverrideCommitsBlockedEpisode(t *testing.T) {
	class := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
	daemon, workspace, policy := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{
		PolicyID:     "decision-override-policy",
		EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
		Requirements: []decisioncore.DecisionRequirement{{
			RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge,
			CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2},
		}},
		OverridePolicy: decisioncore.OverridePolicy{Allowed: true, RequiredAuthorityClass: &class},
	})
	createDecisionEpisodeAndCandidate(t, daemon, "episode-override", "candidate-override")
	projection := inspectDecisionNative(t, daemon, "episode-override", "candidate-override")
	if projection.Protocol.Gate != decisioncore.GateBlocked {
		t.Fatalf("pre-override gate=%s", projection.Protocol.Gate)
	}
	assertDecisionCommitRejected(t, daemon, "episode-override", "candidate-override", policy.PolicyDigest, projection.ProjectionDigest)

	scope := decisioncore.AuthorityScope{RepositoryID: string(workspace.RepositoryID), EpisodeID: "episode-override", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}
	authority := callDecisionNative(t, daemon, "decision.authority.materialize", ipcadapter.DecisionRequestV1{
		AuthorityRequest: &ipcadapter.DecisionAuthorityMaterializeInputV1{RequiredAuthorityClass: class, RequiredScope: scope},
	})
	if authority.AuthorityStatus != decisioncore.QualificationQualified || authority.Attestation == nil {
		t.Fatalf("authority=%#v status=%s", authority.Attestation, authority.AuthorityStatus)
	}
	if authority.Attestation.Scope != scope || !authority.Attestation.AuthorityClass.Equal(class) {
		t.Fatalf("attestation=%#v", authority.Attestation)
	}

	override := callDecisionNative(t, daemon, "decision.override.create", ipcadapter.DecisionRequestV1{
		EpisodeID: "episode-override", CandidateID: "candidate-override", ExpectedPolicyDigest: policy.PolicyDigest,
		ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: projection.Protocol.BlockingRequirementDigest,
		AuthorityAttestationRef: authority.Attestation.AttestationID, Reason: "explicit acceptance override",
	})
	if override.Override == nil || override.Override.BlockingRequirementDigest != projection.Protocol.BlockingRequirementDigest {
		t.Fatalf("override=%#v", override.Override)
	}

	committed := callDecisionNative(t, daemon, "decision.selection.commit", ipcadapter.DecisionRequestV1{
		EpisodeID: "episode-override", CandidateID: "candidate-override", ActorRef: "acceptance-actor",
		ExpectedPolicyDigest: policy.PolicyDigest, ExpectedProjectionDigest: projection.ProjectionDigest,
		OverrideRef: override.Override.OverrideID, IdempotencyKey: "override-commit",
	})
	if committed.Selection == nil || committed.Selection.OverrideAuthorization == nil || committed.Selection.OverrideRef != override.Override.OverrideID {
		t.Fatalf("override selection=%#v", committed.Selection)
	}
	final := inspectDecisionNative(t, daemon, "episode-override", "candidate-override")
	if final.EpisodeState != decisioncore.EpisodeCommitted || final.Protocol.Gate != decisioncore.GateBlocked {
		t.Fatalf("override final=%#v", final)
	}
}

func TestDecisionProtocolNativeOverrideRejectsStaleBlockerAndWrongAuthority(t *testing.T) {
	class := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
	daemon, workspace, policy := startDecisionAcceptanceDaemon(t, decisioncore.PolicyContent{
		PolicyID:       "decision-override-reject-policy",
		EpisodeKinds:   []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
		Requirements:   []decisioncore.DecisionRequirement{{RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge, CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}},
		OverridePolicy: decisioncore.OverridePolicy{Allowed: true, RequiredAuthorityClass: &class},
	})
	createDecisionEpisodeAndCandidate(t, daemon, "episode-override-reject", "candidate-override-reject")
	projection := inspectDecisionNative(t, daemon, "episode-override-reject", "candidate-override-reject")

	stale := callDecisionRawNative(t, daemon, "decision.override.create", ipcadapter.DecisionRequestV1{
		EpisodeID: "episode-override-reject", CandidateID: "candidate-override-reject", ExpectedPolicyDigest: policy.PolicyDigest,
		ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: "block_0000000000000000000000000000000000000000000000000000000000000000",
		AuthorityAttestationRef: "missing-attestation", Reason: "stale",
	})
	if stale.OK {
		t.Fatal("stale blocker digest admitted")
	}

	wrongClass := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "repository_owner", Version: 1}
	wrong := callDecisionNative(t, daemon, "decision.authority.materialize", ipcadapter.DecisionRequestV1{
		AuthorityRequest: &ipcadapter.DecisionAuthorityMaterializeInputV1{RequiredAuthorityClass: wrongClass, RequiredScope: decisioncore.AuthorityScope{RepositoryID: string(workspace.RepositoryID), EpisodeID: "episode-override-reject", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}},
	})
	if wrong.AuthorityStatus == decisioncore.QualificationQualified || wrong.Attestation != nil {
		t.Fatalf("wrong class qualified: %#v", wrong)
	}

	callDecisionNative(t, daemon, "decision.create", ipcadapter.DecisionRequestV1{EpisodeID: "other-episode", EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "acceptance-actor"})
	wrongScope := callDecisionNative(t, daemon, "decision.authority.materialize", ipcadapter.DecisionRequestV1{
		AuthorityRequest: &ipcadapter.DecisionAuthorityMaterializeInputV1{RequiredAuthorityClass: class, RequiredScope: decisioncore.AuthorityScope{RepositoryID: string(workspace.RepositoryID), EpisodeID: "other-episode", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}},
	})
	if wrongScope.AuthorityStatus != decisioncore.QualificationQualified || wrongScope.Attestation == nil {
		t.Fatalf("materialization should qualify exact requested scope independently: %#v", wrongScope)
	}
	misScoped := callDecisionRawNative(t, daemon, "decision.override.create", ipcadapter.DecisionRequestV1{
		EpisodeID: "episode-override-reject", CandidateID: "candidate-override-reject", ExpectedPolicyDigest: policy.PolicyDigest,
		ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: projection.Protocol.BlockingRequirementDigest,
		AuthorityAttestationRef: wrongScope.Attestation.AttestationID, Reason: "wrong scope",
	})
	if misScoped.OK {
		t.Fatal("wrong-scope attestation admitted")
	}
}

type decisionAuthorityResolverState struct {
	materialized decisioncore.MaterializedAuthority
	qualified    decisioncore.DecisionAuthorityQualification
	qualifyCalls int
}

func (r *decisionAuthorityResolverState) MaterializeDecisionAuthority(context.Context, decisionapp.MaterializeAuthorityRequest) (decisioncore.MaterializedAuthority, error) {
	return r.materialized, nil
}

func (r *decisionAuthorityResolverState) QualifyDecisionAuthority(context.Context, decisionapp.QualifyAuthorityRequest) (decisioncore.DecisionAuthorityQualification, error) {
	r.qualifyCalls++
	return r.qualified, nil
}

func TestDecisionProtocolOverrideRevalidatesDynamicAuthorityUntilDurableCommit(t *testing.T) {
	for _, status := range []decisioncore.QualificationStatus{decisioncore.QualificationUnavailable, decisioncore.QualificationRevoked, decisioncore.QualificationExpired} {
		t.Run(string(status), func(t *testing.T) {
			service, resolver, policy, projection, attestation := newDecisionAuthorityAcceptanceService(t)
			resolver.qualified = decisionAuthorityQualification(attestation, status)
			override, err := service.CreateOverride(context.Background(), decisionapp.CreateOverrideRequest{
				EpisodeID: "episode-authority", CandidateID: "candidate-authority", ExpectedPolicyDigest: policy.PolicyDigest,
				ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: projection.Protocol.BlockingRequirementDigest,
				AuthorityAttestationRef: attestation.AttestationID, Reason: "dynamic authority test",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.CommitSelection(context.Background(), decisionapp.CommitSelectionRequest{
				EpisodeID: "episode-authority", CandidateID: "candidate-authority", ActorRef: "acceptance-actor", ExpectedPolicyDigest: policy.PolicyDigest,
				ExpectedProjectionDigest: projection.ProjectionDigest, OverrideRef: override.OverrideID, IdempotencyKey: "authority-" + string(status),
			})
			if err == nil {
				t.Fatalf("%s authority committed", status)
			}
		})
	}
}

func TestDecisionProtocolDurableOverrideCommitSurvivesLaterRevocation(t *testing.T) {
	service, resolver, policy, projection, attestation := newDecisionAuthorityAcceptanceService(t)
	resolver.qualified = decisionAuthorityQualification(attestation, decisioncore.QualificationQualified)
	override, err := service.CreateOverride(context.Background(), decisionapp.CreateOverrideRequest{
		EpisodeID: "episode-authority", CandidateID: "candidate-authority", ExpectedPolicyDigest: policy.PolicyDigest,
		ExpectedProjectionDigest: projection.ProjectionDigest, BlockingRequirementDigest: projection.Protocol.BlockingRequirementDigest,
		AuthorityAttestationRef: attestation.AttestationID, Reason: "durable override",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := decisionapp.CommitSelectionRequest{
		EpisodeID: "episode-authority", CandidateID: "candidate-authority", ActorRef: "acceptance-actor", ExpectedPolicyDigest: policy.PolicyDigest,
		ExpectedProjectionDigest: projection.ProjectionDigest, OverrideRef: override.OverrideID, IdempotencyKey: "durable-authority-commit",
	}
	first, err := service.CommitSelection(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.OverrideAuthorization == nil || resolver.qualifyCalls != 1 {
		t.Fatalf("first=%#v qualify_calls=%d", first, resolver.qualifyCalls)
	}
	resolver.qualified = decisionAuthorityQualification(attestation, decisioncore.QualificationRevoked)
	replay, err := service.CommitSelection(context.Background(), req)
	if err != nil {
		t.Fatalf("durable replay requalified/rejected: %v", err)
	}
	if replay.CommitID != first.CommitID || resolver.qualifyCalls != 1 {
		t.Fatalf("replay=%#v first=%#v qualify_calls=%d", replay, first, resolver.qualifyCalls)
	}
}

func newDecisionAuthorityAcceptanceService(t *testing.T) (*decisionapp.Service, *decisionAuthorityResolverState, decisioncore.PolicySnapshot, decisioncore.DecisionProjection, decisioncore.DecisionAuthorityAttestation) {
	t.Helper()
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	canonical := storeadapter.NewDecisionProtocolStore(repository)
	ws := decisionSecurityWorkspace()
	snapshot := decisionSecuritySnapshot(t, ws)
	workspaces := decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}
	resolver := &decisionAuthorityResolverState{}
	service, err := decisionapp.NewRuntimeService(canonical, decisionActivationGenerationSource{workspaces: workspaces, snapshots: decisionFreshSnapshotterFake{snapshot: snapshot}}, decisionapp.EpisodeDependencies{
		Mutations: canonical, Experiments: decisionExperimentStore{canonical: canonical, repository: repository}, Ledger: canonical, Workspaces: workspaces, Snapshots: decisionFreshSnapshotterFake{snapshot: snapshot},
		Receipts: decisionReceiptSource{store: repository}, Structured: decisionStructuredSourceFake{}, Verification: decisionVerificationSource{store: repository, candidates: decisionEvidenceSourceFake{}},
		Assessments: canonical, Selections: canonical, Authorities: canonical, AuthorityResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	class := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
	content := decisioncore.PolicyContent{
		PolicyID: "authority-policy", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
		Requirements:   []decisioncore.DecisionRequirement{{RequirementID: "challenge", Kind: decisioncore.RequirementCandidateChallenge, CandidateChallenge: &decisioncore.CandidateChallengeRequirement{MinimumDistinctLineages: 2}}},
		OverridePolicy: decisioncore.OverridePolicy{Allowed: true, RequiredAuthorityClass: &class},
	}
	policy, err := service.PutPolicySnapshot(context.Background(), decisionapp.PutPolicySnapshotRequest{RepositoryID: string(ws.RepositoryID), Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivatePolicy(context.Background(), decisionapp.ActivatePolicyRequest{RepositoryID: string(ws.RepositoryID), ActivationID: "authority-activation", PolicyDigest: policy.PolicyDigest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ExpectedPreviousPolicyDigest: "absent", ActorRef: "acceptance-actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEpisode(context.Background(), decisionapp.CreateEpisodeRequest{EpisodeID: "episode-authority", Kind: decisioncore.EpisodeDiagnosis, RepositoryID: string(ws.RepositoryID), WorkspaceID: string(ws.ID), ActorRef: "acceptance-actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateCandidateInput(context.Background(), decisionapp.CreateCandidateInputRequest{EpisodeID: "episode-authority", CandidateID: "candidate-authority", SemanticClaim: "candidate", ActorRef: "acceptance-actor"}); err != nil {
		t.Fatal(err)
	}
	projection, err := service.Project(context.Background(), "episode-authority", "candidate-authority")
	if err != nil {
		t.Fatal(err)
	}
	scope := decisioncore.AuthorityScope{RepositoryID: string(ws.RepositoryID), EpisodeID: "episode-authority", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}
	now := time.Unix(100, 0).UTC()
	resolver.materialized = decisioncore.MaterializedAuthority{Status: decisioncore.QualificationQualified, ActorRef: "shellbeam:explicit_caller:uid:501", AuthorityClass: class, Scope: scope, Resolver: decisioncore.ResolverRef{ProviderID: "test-authority", ProviderVersion: "1", CapabilityVersion: "v1"}, ValidatedAt: now, ProvenanceRef: "test:trusted"}
	materialized, err := service.MaterializeAuthority(context.Background(), decisionapp.MaterializeAuthorityRequest{ActorRef: resolver.materialized.ActorRef, RequiredAuthorityClass: class, RequiredScope: scope})
	if err != nil || materialized.Attestation == nil {
		t.Fatalf("materialize=%#v err=%v", materialized, err)
	}
	return service, resolver, policy, projection, *materialized.Attestation
}

func decisionAuthorityQualification(att decisioncore.DecisionAuthorityAttestation, status decisioncore.QualificationStatus) decisioncore.DecisionAuthorityQualification {
	q := decisioncore.DecisionAuthorityQualification{Status: status, AttestationID: att.AttestationID, Resolver: decisioncore.ResolverRef{ProviderID: "test-authority", ProviderVersion: "2", CapabilityVersion: "v1"}, ValidatedAt: time.Unix(200, 0).UTC()}
	if status == decisioncore.QualificationQualified {
		q.ActorRef, q.AuthorityClass = att.ActorRef, att.AuthorityClass
	}
	return q
}

func decisionSecurityWorkspace() workspacecore.Workspace {
	return workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000071", RepositoryID: "repo_01K00000000000000000000071", Label: "security", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
}

func decisionSecuritySnapshot(t *testing.T, ws workspacecore.Workspace) workspacecore.FastSnapshot {
	t.Helper()
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: strings.Repeat("a", 40), Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: strings.Repeat("b", 64)}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	snapshot, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
