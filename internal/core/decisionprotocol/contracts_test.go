package decisionprotocol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCanonicalRecordKindsAreExactlyFrozenV1Set(t *testing.T) {
	want := []RecordKind{
		RecordPolicySnapshot, RecordPolicyActivation, RecordEpisode, RecordCandidate,
		RecordExperiment, RecordExperimentSeal, RecordPredictionBinding,
		RecordExperimentExecutionLink, RecordExperimentObservationBinding,
		RecordExperimentClosure, RecordExperimentAbort, RecordVerifierAssessment,
		RecordSelectionProposal, RecordAuthorityAttestation, RecordOverride,
		RecordSelectionCommit, RecordClosure,
	}
	if got := CanonicalRecordKinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds=%v want=%v", got, want)
	}
}

func TestCanonicalRecordEnvelopeValidationRejectsZeroSequence(t *testing.T) {
	body, err := json.Marshal(validEpisode())
	if err != nil {
		t.Fatal(err)
	}
	env := CanonicalRecordEnvelope{SchemaVersion: 1, CanonicalRecordSeq: 0, Kind: RecordEpisode, Body: body}
	if err := env.Validate(); err == nil {
		t.Fatal("accepted zero canonical_record_seq")
	}
}

func TestOverridePolicyValidationIsExact(t *testing.T) {
	if err := (OverridePolicy{Allowed: true}).Validate(); err == nil {
		t.Fatal("allowed override accepted without authority class")
	}
	if err := (OverridePolicy{Allowed: false, RequiredAuthorityClass: &AuthorityClass{Domain: "repo", ClassID: "owner", Version: 1}}).Validate(); err == nil {
		t.Fatal("disabled override accepted authority class")
	}
	if err := (OverridePolicy{Allowed: true, RequiredAuthorityClass: &AuthorityClass{Domain: "repo", ClassID: "owner", Version: 1}}).Validate(); err != nil {
		t.Fatalf("valid override policy rejected: %v", err)
	}
}

func TestAbortPhaseControlsExecutionLinkPresence(t *testing.T) {
	before := validAbort()
	before.Phase = AbortBeforeExecution
	before.ExecutionLinkID = LinkID("link-1")
	if err := before.Validate(); err == nil {
		t.Fatal("before-execution abort accepted link")
	}

	after := validAbort()
	after.Phase = AbortAfterExecutionLink
	after.ExecutionLinkID = ""
	if err := after.Validate(); err == nil {
		t.Fatal("after-link abort accepted missing link")
	}
}

func TestVerifierQualificationPresenceIsPaired(t *testing.T) {
	a := validAssessment()
	a.QualifiedContextClass = ContextIndependentModel
	a.ContextQualification = nil
	if err := a.Validate(); err == nil {
		t.Fatal("qualified context accepted without qualification")
	}

	a = validAssessment()
	a.ContextQualification = &ContextQualification{ProviderID: "provider", ProviderVersion: "1", CapabilityVersion: "1", QualifiedAt: fixedTime()}
	if err := a.Validate(); err == nil {
		t.Fatal("qualification accepted without qualified context")
	}
}

func TestAuthorityClassMatchingIsExact(t *testing.T) {
	required := AuthorityClass{Domain: "repo", ClassID: "owner", Version: 1}
	if !required.Equal(required) {
		t.Fatal("class must equal itself")
	}
	if required.Equal(AuthorityClass{Domain: "repo", ClassID: "maintainer", Version: 1}) {
		t.Fatal("owner matched maintainer")
	}
	if required.Equal(AuthorityClass{Domain: "repo", ClassID: "owner", Version: 2}) {
		t.Fatal("authority version ignored")
	}
}

func TestAllCanonicalRecordBodiesValidate(t *testing.T) {
	cases := []struct {
		name string
		v    interface{ Validate() error }
	}{
		{"policy_snapshot", validPolicySnapshot()},
		{"policy_activation", validPolicyActivation()},
		{"episode", validEpisode()},
		{"candidate", validCandidate()},
		{"experiment", validExperiment()},
		{"experiment_seal", validSeal()},
		{"prediction_binding", validPrediction()},
		{"execution_link", validExecutionLink()},
		{"observation_binding", validObservationBinding()},
		{"experiment_closure", validExperimentClosure()},
		{"experiment_abort", validAbort()},
		{"verifier_assessment", validAssessment()},
		{"selection_proposal", validProposal()},
		{"authority_attestation", validAttestation()},
		{"override", validOverride()},
		{"selection_commit", validSelectionCommit()},
		{"closure", validClosure()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.v.Validate(); err != nil {
				t.Fatalf("valid record rejected: %v", err)
			}
		})
	}
}

func TestReasonCodesAreExactlyFrozenV1Set(t *testing.T) {
	want := []ReasonCode{
		ReasonCandidateRevisionConflict, ReasonExperimentAlreadySealed,
		ReasonExperimentExecutionLimitReached, ReasonExperimentNotSealed,
		ReasonObservationNotSettled, ReasonExperimentObservationBindingConflict,
		ReasonStaleEpisodeSourceGeneration, ReasonProjectionConflict, ReasonPolicyConflict,
		ReasonEpisodeTerminalConflict, ReasonTerminalSelectionConflict, ReasonIdempotencyConflict,
		ReasonProtocolBlocked, ReasonProtocolIndeterminate, ReasonOverrideScopeStale,
		ReasonOverrideAuthorityNotAdmissible, ReasonAuthorityRequirementUnavailable,
	}
	if got := ReasonCodes(); !slices.Equal(got, want) {
		t.Fatalf("reason codes=%v want=%v", got, want)
	}
}

func validPolicyContent() PolicyContent {
	return PolicyContent{
		PolicyID:     "policy-v1",
		EpisodeKinds: []EpisodeKind{EpisodeDiagnosis},
		Requirements: []DecisionRequirement{{
			RequirementID:      "challenge",
			Kind:               RequirementCandidateChallenge,
			CandidateChallenge: &CandidateChallengeRequirement{MinimumDistinctLineages: 2},
		}},
		OverridePolicy: OverridePolicy{Allowed: false},
	}
}

func validPolicySnapshot() DecisionPolicySnapshot {
	content := validPolicyContent()
	digest, _ := PolicyDigest(content)
	return DecisionPolicySnapshot{SchemaVersion: 1, RepositoryID: "repo-1", PolicyDigest: digest, Content: content}
}

func validPolicyActivation() DecisionPolicyActivation {
	return DecisionPolicyActivation{ActivationID: "act-1", RepositoryID: "repo-1", PolicyDigest: "pol_" + strings.Repeat("a", 64), ProposalGeneration: "gen_" + strings.Repeat("b", 64), ActivationGeneration: "gen_" + strings.Repeat("c", 64), Authority: AuthorityExplicitCaller, ActorRef: "actor-1", ActivatedAt: fixedTime()}
}

func validEpisode() DecisionEpisode {
	return DecisionEpisode{SchemaVersion: 1, EpisodeID: "ep-1", EpisodeKind: EpisodeDiagnosis, RepositoryID: "repo-1", WorkspaceID: "ws-1", Baseline: EpisodeBaseline{SourceGeneration: "src-1"}, PolicyBinding: EpisodePolicyBinding{PolicyID: "policy-v1", PolicyDigest: "pol_" + strings.Repeat("a", 64), ActivationRef: "act-1"}, CreatedByActorRef: "actor-1", CreatedAt: fixedTime()}
}

func validCandidate() DecisionCandidate {
	return DecisionCandidate{CandidateID: "cand-1", EpisodeID: "ep-1", SemanticClaim: "claim", DeclaredByActorRef: "actor-1", DeclaredAt: fixedTime()}
}
func validExperiment() DecisionExperiment {
	return DecisionExperiment{SchemaVersion: 1, ExperimentID: "exp-1", EpisodeID: "ep-1", DeclaredByActorRef: "actor-1", DeclaredAt: fixedTime()}
}
func validSeal() ExperimentSeal {
	return ExperimentSeal{ExperimentID: "exp-1", SourceGeneration: "src-1", SealedPredictionDigest: "pred_" + strings.Repeat("a", 64), BaseProjectionCutRef: DecisionProjectionCutRef{EpisodeID: "ep-1", CanonicalRecordHighWater: 1}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: fixedTime()}
}
func validPrediction() PredictionBinding {
	return PredictionBinding{PredictionID: "pred-1", EpisodeID: "ep-1", ExperimentID: "exp-1", CandidateID: "cand-1", Role: PredictionRequired, Predicate: ObservationPredicate{Kind: PredicateOperationOutcome, OperationOutcome: &OperationOutcomePredicate{ExpectedOutcome: OperationSuccess}}, SourceGeneration: "src-1", CommittedAt: fixedTime()}
}
func validExecutionLink() ExperimentExecutionLink {
	return ExperimentExecutionLink{SchemaVersion: 1, LinkID: "link-1", ExperimentID: "exp-1", OperationID: "op-1", SessionID: "sess-1", WorkspaceID: "ws-1", SourceGeneration: "src-1", AcceptedRequestFingerprint: strings.Repeat("a", 64), AcceptedExecutionFingerprint: strings.Repeat("b", 64), AcceptedObservationBindingFingerprint: strings.Repeat("c", 64), AdmittedAt: fixedTime()}
}
func validObservationBinding() ExperimentObservationBinding {
	return ExperimentObservationBinding{SchemaVersion: 1, BindingID: "bind-1", ExperimentID: "exp-1", OperationID: "op-1", SourceGeneration: "src-1", ObservationSemanticsVersion: 1, DerivationCutDigest: "cut_" + strings.Repeat("d", 64), PredictionResults: []PredictionResult{{PredictionID: "pred-1", Status: PredictionMatch}}, MaterializedAt: fixedTime()}
}
func validExperimentClosure() ExperimentClosure {
	return ExperimentClosure{SchemaVersion: 1, ClosureID: "xclose-1", ExperimentID: "exp-1", ObservationBindingID: "bind-1", ClosedByActorRef: "actor-1", ClosedAt: fixedTime()}
}
func validAbort() ExperimentAbort {
	return ExperimentAbort{SchemaVersion: 1, AbortID: "abort-1", ExperimentID: "exp-1", Phase: AbortBeforeExecution, Reason: "stop", AbortedByActorRef: "actor-1", AbortedAt: fixedTime()}
}
func validAssessment() VerifierAssessment {
	return VerifierAssessment{AssessmentID: "assess-1", EpisodeID: "ep-1", ActorRef: "actor-1", DeclaredContextClass: ContextSameContext, PreferredCandidates: []CandidateID{"cand-1"}, Rationale: "prefer", CreatedAt: fixedTime()}
}
func validProposal() SelectionProposal {
	return SelectionProposal{ProposalID: "proposal-1", EpisodeID: "ep-1", CandidateID: "cand-1", ActorRef: "actor-1", CreatedAt: fixedTime()}
}
func validAttestation() DecisionAuthorityAttestation {
	return DecisionAuthorityAttestation{SchemaVersion: 1, AttestationID: "att-1", ActorRef: "actor-1", AuthorityClass: AuthorityClass{Domain: "repo", ClassID: "owner", Version: 1}, Scope: AuthorityScope{RepositoryID: "repo-1", EpisodeID: "ep-1", ActionKind: AuthorityActionCommitSelectionOverride}, Resolver: ResolverRef{ProviderID: "provider", ProviderVersion: "1", CapabilityVersion: "1"}, IssuedAt: fixedTime(), ProvenanceRef: "prov-1"}
}
func validOverride() DecisionOverride {
	return DecisionOverride{OverrideID: "override-1", EpisodeID: "ep-1", CandidateID: "cand-1", PolicyDigest: "pol_" + strings.Repeat("a", 64), ProjectionDigest: "proj_" + strings.Repeat("b", 64), BlockingRequirementDigest: "block_" + strings.Repeat("c", 64), BlockingRequirements: []string{"challenge"}, ActorRef: "actor-1", AuthorityAttestationRef: "att-1", Reason: "authorized exception", CreatedAt: fixedTime()}
}
func validSelectionCommit() SelectionCommit {
	return SelectionCommit{CommitID: "commit-1", EpisodeID: "ep-1", CandidateID: "cand-1", PolicyDigest: "pol_" + strings.Repeat("a", 64), ProjectionDigest: "proj_" + strings.Repeat("b", 64), SourceGeneration: "src-1", IdempotencyKey: "idem-1", SemanticIntentFingerprint: "sel_" + strings.Repeat("d", 64), CommittedByActorRef: "actor-1", CommittedAt: fixedTime()}
}
func validClosure() DecisionClosure {
	return DecisionClosure{EpisodeID: "ep-1", Kind: ClosureUnresolved, Reason: "not enough evidence", UnresolvedDimensions: []string{"root-cause"}, ActorRef: "actor-1", ProjectionDigest: "proj_" + strings.Repeat("b", 64), ClosedAt: fixedTime()}
}
func fixedTime() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }

func TestPredictionEvaluationRequirementRejectsObservationTargetRoleInV1(t *testing.T) {
	r := DecisionRequirement{
		RequirementID: "prediction-eval",
		Kind:          RequirementPredictionEvaluation,
		PredictionEvaluation: &PredictionEvaluationRequirement{
			MinimumEvaluatedPredictions: 1,
			Roles:                       []PredictionRole{PredictionObservationTarget},
		},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("V1 prediction-evaluation requirement accepted OBSERVATION_TARGET role")
	}
}

func TestVerifierAssessmentRequirementRejectsDistinctActorRefsTrueInV1(t *testing.T) {
	r := DecisionRequirement{
		RequirementID: "verifier",
		Kind:          RequirementVerifierAssessment,
		VerifierAssessment: &VerifierAssessmentRequirement{
			MinimumSupportingAssessments: 1,
			DistinctActorRefs:            true,
		},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("V1 verifier-assessment requirement accepted configurable distinct_actor_refs")
	}
}
