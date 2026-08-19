package decisionprotocol

import (
	"context"
	"fmt"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type MaterializeAuthorityResult struct {
	Status      core.QualificationStatus           `json:"status"`
	Attestation *core.DecisionAuthorityAttestation `json:"attestation,omitempty"`
}

func (s *Service) MaterializeAuthority(ctx context.Context, req MaterializeAuthorityRequest) (MaterializeAuthorityResult, error) {
	if s == nil || s.authorityResolver == nil || s.authorities == nil {
		return MaterializeAuthorityResult{}, fmt.Errorf("decision authority materialization unavailable")
	}
	if req.ActorRef == "" || len(req.ActorRef) > 192 || req.RequiredAuthorityClass.Validate() != nil || req.RequiredScope.Validate() != nil {
		return MaterializeAuthorityResult{}, fmt.Errorf("invalid authority materialization request")
	}
	materialized, err := s.authorityResolver.MaterializeDecisionAuthority(ctx, req)
	if err != nil {
		return MaterializeAuthorityResult{}, err
	}
	if err := materialized.Validate(); err != nil {
		return MaterializeAuthorityResult{}, err
	}
	result := MaterializeAuthorityResult{Status: materialized.Status}
	if materialized.Status != core.QualificationQualified {
		return result, nil
	}
	if materialized.ActorRef != req.ActorRef || !materialized.AuthorityClass.Equal(req.RequiredAuthorityClass) || !authorityScopeEqual(materialized.Scope, req.RequiredScope) {
		return MaterializeAuthorityResult{}, fmt.Errorf("qualified authority result does not match requested principal/class/scope")
	}
	attestation := core.DecisionAuthorityAttestation{
		SchemaVersion: 1,
		AttestationID: semanticRecordID("attestation", materialized.ActorRef, materialized.AuthorityClass.Domain, materialized.AuthorityClass.ClassID, fmt.Sprint(materialized.AuthorityClass.Version), materialized.Scope.RepositoryID, string(materialized.Scope.EpisodeID), string(materialized.Scope.ActionKind), materialized.Resolver.ProviderID, materialized.Resolver.ProviderVersion, materialized.Resolver.CapabilityVersion, materialized.ValidatedAt.UTC().Format(time.RFC3339Nano), materialized.QualificationCutDigest, materialized.ProvenanceRef),
		ActorRef:      materialized.ActorRef, AuthorityClass: materialized.AuthorityClass, Scope: materialized.Scope, Resolver: materialized.Resolver,
		IssuedAt: materialized.ValidatedAt.UTC(), ExpiresAt: materialized.ExpiresAt, ProvenanceRef: materialized.ProvenanceRef,
	}
	if err := attestation.Validate(); err != nil {
		return MaterializeAuthorityResult{}, err
	}
	if _, _, err := s.authorities.PutAuthorityAttestation(ctx, attestation); err != nil {
		return MaterializeAuthorityResult{}, err
	}
	result.Attestation = &attestation
	return result, nil
}

func authorityScopeEqual(a, b core.AuthorityScope) bool {
	return a.RepositoryID == b.RepositoryID && a.EpisodeID == b.EpisodeID && a.ActionKind == b.ActionKind
}

type CreateOverrideRequest struct {
	EpisodeID                 core.EpisodeID
	CandidateID               core.CandidateID
	ExpectedPolicyDigest      string
	ExpectedProjectionDigest  string
	BlockingRequirementDigest string
	AuthorityAttestationRef   string
	Reason                    string
}

func (s *Service) CreateOverride(ctx context.Context, req CreateOverrideRequest) (core.DecisionOverride, error) {
	if s == nil || s.authorities == nil || s.mutations == nil || s.policies == nil {
		return core.DecisionOverride{}, fmt.Errorf("decision override dependencies unavailable")
	}
	episode, found, err := s.mutations.FindEpisode(ctx, req.EpisodeID)
	if err != nil {
		return core.DecisionOverride{}, err
	}
	if !found {
		return core.DecisionOverride{}, fmt.Errorf("decision episode unavailable")
	}
	if req.ExpectedPolicyDigest != episode.PolicyBinding.PolicyDigest {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonPolicyConflict, "override policy digest mismatch")
	}
	projection, err := s.Project(ctx, req.EpisodeID, req.CandidateID)
	if err != nil {
		return core.DecisionOverride{}, err
	}
	if projection.EpisodeState != core.EpisodeOpen {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonEpisodeTerminalConflict, "episode already terminal")
	}
	if req.ExpectedProjectionDigest != projection.ProjectionDigest {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonProjectionConflict, "override projection digest is stale")
	}
	if req.BlockingRequirementDigest != projection.Protocol.BlockingRequirementDigest || projection.Protocol.Gate == core.GateClear {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonOverrideScopeStale, "override blocker set is stale or empty")
	}
	policy, found, err := s.policies.LoadPolicySnapshot(ctx, episode.RepositoryID, episode.PolicyBinding.PolicyDigest)
	if err != nil {
		return core.DecisionOverride{}, err
	}
	if !found || !policy.Content.OverridePolicy.Allowed || policy.Content.OverridePolicy.RequiredAuthorityClass == nil {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "override policy disabled")
	}
	attestation, found, err := s.authorities.FindAuthorityAttestation(ctx, req.AuthorityAttestationRef)
	if err != nil {
		return core.DecisionOverride{}, err
	}
	if !found || !attestation.AuthorityClass.Equal(*policy.Content.OverridePolicy.RequiredAuthorityClass) || !authorityScopeEqual(attestation.Scope, authorityScopeForEpisode(episode)) {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "attestation class/scope does not authorize override intent")
	}
	blocking := overrideBlockingRequirements(projection.Protocol)
	if len(blocking) == 0 {
		return core.DecisionOverride{}, core.NewReasonError(core.ReasonOverrideScopeStale, "override has no current blockers")
	}
	now := s.now().UTC()
	override := core.DecisionOverride{OverrideID: semanticRecordID("override", string(req.EpisodeID), string(req.CandidateID), req.ExpectedPolicyDigest, req.ExpectedProjectionDigest, req.BlockingRequirementDigest, req.AuthorityAttestationRef, req.Reason, now.Format(time.RFC3339Nano)), EpisodeID: req.EpisodeID, CandidateID: req.CandidateID, PolicyDigest: req.ExpectedPolicyDigest, ProjectionDigest: req.ExpectedProjectionDigest, BlockingRequirementDigest: req.BlockingRequirementDigest, BlockingRequirements: blocking, ActorRef: attestation.ActorRef, AuthorityAttestationRef: attestation.AttestationID, Reason: req.Reason, CreatedAt: now}
	if err := override.Validate(); err != nil {
		return core.DecisionOverride{}, err
	}
	if _, _, err := s.authorities.RecordOverride(ctx, override); err != nil {
		return core.DecisionOverride{}, err
	}
	return override, nil
}

func (s *Service) authorizeOverrideCommit(ctx context.Context, episode core.Episode, projection core.DecisionProjection, overrideRef string) (*core.OverrideAuthorization, error) {
	if s.authorities == nil || s.policies == nil {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "override authority state unavailable")
	}
	policy, found, err := s.policies.LoadPolicySnapshot(ctx, episode.RepositoryID, episode.PolicyBinding.PolicyDigest)
	if err != nil {
		return nil, err
	}
	if !found || !policy.Content.OverridePolicy.Allowed || policy.Content.OverridePolicy.RequiredAuthorityClass == nil {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "override policy disabled")
	}
	override, found, err := s.authorities.FindOverride(ctx, overrideRef)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "override intent unavailable")
	}
	if override.EpisodeID != episode.EpisodeID || override.CandidateID != projection.CandidateID || override.PolicyDigest != episode.PolicyBinding.PolicyDigest || override.ProjectionDigest != projection.ProjectionDigest || override.BlockingRequirementDigest != projection.Protocol.BlockingRequirementDigest {
		return nil, core.NewReasonError(core.ReasonOverrideScopeStale, "override no longer covers current projection/blockers")
	}
	attestation, found, err := s.authorities.FindAuthorityAttestation(ctx, override.AuthorityAttestationRef)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "authority attestation unavailable")
	}
	requiredClass := *policy.Content.OverridePolicy.RequiredAuthorityClass
	requiredScope := authorityScopeForEpisode(episode)
	if attestation.ActorRef != override.ActorRef || !attestation.AuthorityClass.Equal(requiredClass) || !authorityScopeEqual(attestation.Scope, requiredScope) {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "attestation no longer matches override policy/scope")
	}
	if s.authorityResolver == nil {
		return nil, core.NewReasonError(core.ReasonAuthorityRequirementUnavailable, "override authority resolver unavailable")
	}
	qualification, err := s.authorityResolver.QualifyDecisionAuthority(ctx, QualifyAuthorityRequest{AttestationID: attestation.AttestationID, ExpectedActorRef: attestation.ActorRef, RequiredAuthorityClass: requiredClass, RequiredScope: requiredScope})
	if err != nil {
		return nil, err
	}
	if err := qualification.Validate(); err != nil {
		return nil, err
	}
	if qualification.Status == core.QualificationUnavailable {
		return nil, core.NewReasonError(core.ReasonAuthorityRequirementUnavailable, "authority resolver unavailable")
	}
	if qualification.Status != core.QualificationQualified {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "authority is not currently qualified")
	}
	if qualification.AttestationID != attestation.AttestationID || qualification.ActorRef != attestation.ActorRef || !qualification.AuthorityClass.Equal(requiredClass) {
		return nil, core.NewReasonError(core.ReasonOverrideAuthorityNotAdmissible, "qualification identity/class mismatch")
	}
	return &core.OverrideAuthorization{AuthorityAttestationRef: attestation.AttestationID, AuthorityClass: qualification.AuthorityClass, ActorRef: qualification.ActorRef, Resolver: qualification.Resolver, ValidatedAt: qualification.ValidatedAt, QualificationCutDigest: qualification.QualificationCutDigest}, nil
}

func authorityScopeForEpisode(ep core.Episode) core.AuthorityScope {
	return core.AuthorityScope{RepositoryID: ep.RepositoryID, EpisodeID: ep.EpisodeID, ActionKind: core.AuthorityActionCommitSelectionOverride}
}

func overrideBlockingRequirements(e core.DecisionProtocolEvaluation) []string {
	out := []string{}
	for _, r := range e.RequirementEvaluations {
		if r.Status != core.RequirementSatisfied {
			out = append(out, "requirement:"+r.RequirementID+":"+string(r.Status))
		}
	}
	for _, b := range e.CandidateContractBlockers {
		out = append(out, "candidate_contract:"+b.Code+":"+string(b.PredictionID))
	}
	return uniqueSortedStrings(out)
}
