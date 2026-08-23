package decisionprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type fakeVerifierQualifier struct {
	result core.ContextQualificationResult
	calls  int
}

func (f *fakeVerifierQualifier) QualifyVerifierContext(context.Context, QualifyVerifierContextRequest) (core.ContextQualificationResult, error) {
	f.calls++
	return f.result, nil
}

func (f *fakeEpisodeLedger) RecordAssessment(_ context.Context, assessment core.VerifierAssessment) (core.CanonicalRecordEnvelope, bool, error) {
	for _, record := range f.records {
		if record.Kind != core.RecordVerifierAssessment {
			continue
		}
		var existing core.VerifierAssessment
		if err := json.Unmarshal(record.Body, &existing); err != nil {
			return core.CanonicalRecordEnvelope{}, false, err
		}
		if existing.AssessmentID == assessment.AssessmentID {
			if reflect.DeepEqual(existing, assessment) {
				return record, false, nil
			}
			return core.CanonicalRecordEnvelope{}, false, fmt.Errorf("assessment identity conflict")
		}
	}
	env, err := f.append(core.RecordVerifierAssessment, assessment)
	return env, err == nil, err
}

func assessmentService(t *testing.T, qualifier VerifierContextQualifier) (*Service, *fakeEpisodeLedger) {
	t.Helper()
	content := task7Policy()
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakePolicyStore{put: true}
	policy.snapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: content}
	policy.currentSnapshot = policy.snapshot
	policy.currentActivation = core.PolicyActivation{ActivationID: "act-assess", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ActivationGeneration: "gen_" + strings.Repeat("b", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	policy.currentOK = true
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Assessments: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}, VerifierQualifier: qualifier})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-assess", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []core.CandidateID{"a", "b"} {
		if _, err := svc.CreateCandidate(context.Background(), core.Candidate{CandidateID: id, EpisodeID: "ep-assess", SemanticClaim: string(id), DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	return svc, ledger
}

func TestCallerDeclaredHumanRemainsUnqualifiedWithoutTrustedQualifier(t *testing.T) {
	svc, _ := assessmentService(t, nil)
	got, err := svc.RecordAssessment(context.Background(), RecordAssessmentRequest{AssessmentID: "as-1", EpisodeID: "ep-assess", ActorRef: "actor", DeclaredContextClass: core.ContextHuman, PreferredCandidates: []core.CandidateID{"a"}, Rationale: "prefer A"})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeclaredContextClass != core.ContextHuman || got.QualifiedContextClass != "" || got.ContextQualification != nil {
		t.Fatalf("assessment=%#v", got)
	}
}

func TestCallerCannotPopulateQualifiedContextFields(t *testing.T) {
	typ := reflect.TypeOf(RecordAssessmentRequest{})
	for _, forbidden := range []string{"QualifiedContextClass", "ContextQualification"} {
		if _, ok := typ.FieldByName(forbidden); ok {
			t.Fatalf("caller request exposes %s", forbidden)
		}
	}
}

func TestTrustedQualifierMaterializesExactContextQualification(t *testing.T) {
	qualifiedAt := time.Unix(30, 0).UTC()
	qualifier := &fakeVerifierQualifier{result: core.ContextQualificationResult{Status: core.ContextQualificationQualified, QualifiedContextClass: core.ContextIndependentModel, Qualification: &core.ContextQualification{ProviderID: "trusted", ProviderVersion: "1", CapabilityVersion: "v1", QualificationCutDigest: "cut_" + strings.Repeat("c", 64), QualifiedAt: qualifiedAt}}}
	svc, _ := assessmentService(t, qualifier)
	got, err := svc.RecordAssessment(context.Background(), RecordAssessmentRequest{AssessmentID: "as-2", EpisodeID: "ep-assess", ActorRef: "actor", DeclaredContextClass: core.ContextIndependentModel, DeclaredProviderIdentity: "caller-claim", PreferredCandidates: []core.CandidateID{"a"}, Rationale: "prefer A"})
	if err != nil {
		t.Fatal(err)
	}
	if qualifier.calls != 1 || got.QualifiedContextClass != core.ContextIndependentModel || got.ContextQualification == nil || got.ContextQualification.ProviderID != "trusted" || !got.ContextQualification.QualifiedAt.Equal(qualifiedAt) {
		t.Fatalf("assessment=%#v calls=%d", got, qualifier.calls)
	}
}

func TestVerifierAssessmentCannotConvertToEvidenceCandidate(t *testing.T) {
	typ := reflect.TypeOf(core.VerifierAssessment{})
	for _, name := range []string{"ToEvidenceCandidate", "EvidenceCandidate", "AsEvidence"} {
		if _, ok := typ.MethodByName(name); ok {
			t.Fatalf("verifier assessment exposes evidence conversion %s", name)
		}
	}
}

func TestVerifierAssessmentRequirementQualificationFold(t *testing.T) {
	req := core.DecisionRequirement{RequirementID: "verify", Kind: core.RequirementVerifierAssessment, VerifierAssessment: &core.VerifierAssessmentRequirement{MinimumSupportingAssessments: 1, RequiredContextClass: core.ContextIndependentModel}}
	facts := core.EvaluationFacts{EpisodeID: "ep-1", CandidateID: "a", VerifierAssessments: []core.VerifierAssessment{{AssessmentID: "as-unresolved", EpisodeID: "ep-1", ActorRef: "actor", DeclaredContextClass: core.ContextIndependentModel, PreferredCandidates: []core.CandidateID{"a"}, Rationale: "x", CreatedAt: time.Unix(1, 0).UTC()}}}
	if got := EvaluateRequirements(task7Policy(req), facts); got.RequirementEvaluations[0].Status != core.RequirementIndeterminate {
		t.Fatalf("unresolved=%#v", got)
	}
	facts.VerifierAssessments[0].QualifiedContextClass = core.ContextHuman
	facts.VerifierAssessments[0].ContextQualification = &core.ContextQualification{ProviderID: "trusted", ProviderVersion: "1", CapabilityVersion: "1", QualifiedAt: time.Unix(2, 0).UTC()}
	if got := EvaluateRequirements(task7Policy(req), facts); got.RequirementEvaluations[0].Status != core.RequirementUnsatisfied {
		t.Fatalf("wrong qualified class=%#v", got)
	}
	facts.VerifierAssessments[0].QualifiedContextClass = core.ContextIndependentModel
	if got := EvaluateRequirements(task7Policy(req), facts); got.RequirementEvaluations[0].Status != core.RequirementSatisfied {
		t.Fatalf("qualified=%#v", got)
	}
}

func TestVerifierSemanticStateChangesProjectionDigestWithoutChangingClearGate(t *testing.T) {
	content := task7Policy(core.DecisionRequirement{RequirementID: "verify", Kind: core.RequirementVerifierAssessment, VerifierAssessment: &core.VerifierAssessmentRequirement{MinimumSupportingAssessments: 1}})
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakePolicyStore{put: true}
	policy.snapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: content}
	policy.currentSnapshot = policy.snapshot
	policy.currentActivation = core.PolicyActivation{ActivationID: "act-digest", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ActivationGeneration: "gen_" + strings.Repeat("b", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	policy.currentOK = true
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Assessments: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-digest", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCandidate(context.Background(), core.Candidate{CandidateID: "a", EpisodeID: "ep-digest", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	first := core.VerifierAssessment{AssessmentID: "as-first", EpisodeID: "ep-digest", ActorRef: "actor-1", DeclaredContextClass: core.ContextSameContext, PreferredCandidates: []core.CandidateID{"a"}, Rationale: "first", CreatedAt: time.Unix(30, 0).UTC()}
	if _, _, err := ledger.RecordAssessment(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	p1, err := svc.Project(context.Background(), "ep-digest", "a")
	if err != nil {
		t.Fatal(err)
	}
	second := core.VerifierAssessment{AssessmentID: "as-second", EpisodeID: "ep-digest", ActorRef: "actor-2", DeclaredContextClass: core.ContextSameContext, PreferredCandidates: []core.CandidateID{"a"}, Rationale: "second", CreatedAt: time.Unix(31, 0).UTC()}
	if _, _, err := ledger.RecordAssessment(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	p2, err := svc.Project(context.Background(), "ep-digest", "a")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Protocol.Gate != core.GateClear || p2.Protocol.Gate != core.GateClear || p1.ProjectionDigest == p2.ProjectionDigest {
		t.Fatalf("p1=%#v p2=%#v", p1, p2)
	}
	replay, created, err := ledger.RecordAssessment(context.Background(), second)
	if err != nil || created || replay.Kind != core.RecordVerifierAssessment {
		t.Fatalf("replay=%#v created=%v err=%v", replay, created, err)
	}
	p3, err := svc.Project(context.Background(), "ep-digest", "a")
	if err != nil {
		t.Fatal(err)
	}
	if p2.ProjectionDigest != p3.ProjectionDigest {
		t.Fatalf("replay changed digest %s -> %s", p2.ProjectionDigest, p3.ProjectionDigest)
	}
}
