package verification

import (
	"context"
	"fmt"
	"sort"
	"sync"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type PolicyState string

const (
	PolicyStateAbsent          PolicyState = "absent"
	PolicyStateEffective       PolicyState = "effective"
	PolicyStateProposalPending PolicyState = "proposal_pending"
	PolicyStateInvalid         PolicyState = "invalid"
	PolicyStateUnsupported     PolicyState = "unsupported"
)

type InspectRequest struct {
	WorkspaceID string     `json:"workspace_id"`
	ActivityID  string     `json:"activity_id,omitempty"`
	Phase       core.Phase `json:"phase"`
}

type ObligationView struct {
	ObligationID       string                       `json:"obligation_id"`
	SourceRuleID       string                       `json:"source_rule_id"`
	Disposition        core.ObligationDisposition   `json:"disposition"`
	EvidenceStatus     core.EvidenceStatus          `json:"evidence_status"`
	SufficiencyBasis   string                       `json:"sufficiency_basis"`
	RequirementResults []core.RequirementEvaluation `json:"requirement_results"`
	EvidenceRefs       []string                     `json:"evidence_refs,omitempty"`
	WaiverID           string                       `json:"waiver_id,omitempty"`
	ReasonCodes        []string                     `json:"reason_codes,omitempty"`
}

type Inspection struct {
	SchemaVersion    int                           `json:"schema_version"`
	Phase            core.Phase                    `json:"phase"`
	RepositoryID     string                        `json:"repository_id"`
	WorkspaceID      string                        `json:"workspace_id"`
	SourceGeneration string                        `json:"source_generation"`
	EffectivePolicy  *core.PolicySummary           `json:"effective_policy,omitempty"`
	ProposedPolicy   *core.PolicyProposalSummary   `json:"proposed_policy,omitempty"`
	PolicyState      PolicyState                   `json:"policy_state"`
	Affected         core.AffectedSurfaceSummary   `json:"affected_surface"`
	Gate             core.GateEvaluation           `json:"gate"`
	Obligations      []core.VerificationObligation `json:"obligations"`
	ObligationViews  []ObligationView              `json:"obligation_views"`
	PolicyGaps       []core.PolicyGap              `json:"policy_gaps,omitempty"`
	CostSummary      []core.BoundRequirementCost   `json:"cost_summary,omitempty"`
}

type PreviewPolicyRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Profile     string `json:"profile,omitempty"`
}

type PolicyPreview struct {
	State        PolicyLoadState      `json:"state"`
	Proposal     *core.PolicyProposal `json:"proposal,omitempty"`
	RenderedTOML string               `json:"rendered_toml,omitempty"`
	Advisories   []string             `json:"advisories,omitempty"`
	RawDigest    string               `json:"raw_digest,omitempty"`
	Code         string               `json:"code,omitempty"`
}

type InspectionService struct {
	workspaces  WorkspaceInspector
	loader      PolicyLoader
	effective   EffectivePolicyStore
	affected    AffectedDeriver
	obligations ObligationDeriver
	waivers     ActiveWaiverReader
	projects    ProjectInspector
	starters    StarterPolicyPreviewer

	evaluationMu    sync.RWMutex
	evaluation      EvaluationSources
	evaluationBound bool
}

func NewInspectionService(workspaces WorkspaceInspector, loader PolicyLoader, effective EffectivePolicyStore, affected AffectedDeriver, obligations ObligationDeriver, waivers ActiveWaiverReader, projects ProjectInspector, starters StarterPolicyPreviewer) *InspectionService {
	return &InspectionService{workspaces: workspaces, loader: loader, effective: effective, affected: affected, obligations: obligations, waivers: waivers, projects: projects, starters: starters}
}

func (s *InspectionService) BindEvaluationSources(sources EvaluationSources) error {
	if s == nil {
		return fmt.Errorf("verification inspection unavailable")
	}
	s.evaluationMu.Lock()
	defer s.evaluationMu.Unlock()
	if s.evaluationBound {
		return fmt.Errorf("verification evaluation sources already bound")
	}
	s.evaluation = sources
	s.evaluationBound = true
	return nil
}

func (s *InspectionService) evaluationSources() (EvaluationSources, bool) {
	s.evaluationMu.RLock()
	defer s.evaluationMu.RUnlock()
	return s.evaluation, s.evaluationBound
}

func (s *InspectionService) Inspect(ctx context.Context, req InspectRequest) (Inspection, error) {
	if s == nil || s.workspaces == nil || s.loader == nil || s.effective == nil || s.affected == nil {
		return Inspection{}, fmt.Errorf("verification inspection unavailable")
	}
	if err := req.Phase.Validate(); err != nil {
		return Inspection{}, err
	}
	ws, err := s.workspaces.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return Inspection{}, err
	}
	affected, err := s.affected.Derive(ctx, AffectedRequest{WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID})
	if err != nil {
		return Inspection{}, err
	}
	load := s.loader.Load(ctx, ws)
	materialized, effectiveSummary, found, err := s.materializedPolicy(ctx, ws.RepositoryID)
	if err != nil {
		return Inspection{}, err
	}
	state := resolvePolicyState(load, found, materialized)
	out := Inspection{
		SchemaVersion: 2, Phase: req.Phase, RepositoryID: string(ws.RepositoryID), WorkspaceID: string(ws.ID),
		SourceGeneration: affected.Surface.SourceGeneration, EffectivePolicy: effectiveSummary,
		ProposedPolicy: proposalSummary(load, found, materialized), PolicyState: state,
		Obligations: []core.VerificationObligation{}, ObligationViews: []ObligationView{},
	}
	if !found {
		out.Affected = affected.Surface.Summary()
		out.Gate, err = GateForPolicyState(state, false, nil, nil)
		return out, err
	}
	classified, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: affected.Surface, EffectivePolicy: materialized})
	if err != nil {
		return Inspection{}, err
	}
	out.Affected = classified.Summary()
	if s.obligations == nil || s.waivers == nil {
		return Inspection{}, fmt.Errorf("verification obligation projection unavailable")
	}
	waivers, err := s.waivers.ActiveWaivers(ctx, WaiverScope{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Generation: classified.SourceGeneration})
	if err != nil {
		return Inspection{}, err
	}
	derived, err := s.obligations.Derive(ctx, ObligationRequest{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Policy: materialized, Surface: classified, ActiveWaivers: waivers})
	if err != nil {
		return Inspection{}, err
	}
	out.Obligations = append([]core.VerificationObligation(nil), derived.Obligations...)
	out.PolicyGaps = append([]core.PolicyGap(nil), derived.PolicyGaps...)

	sources, _ := s.evaluationSources()
	applicable := applicableObligations(out.Obligations)
	candidates := readInspectionCandidates(ctx, sources.Evidence, req)
	currentEnvironment := readCurrentEnvironment(ctx, sources.Environment, req.WorkspaceID, applicable)
	quiescence := readQuiescence(ctx, sources.Quiescence, req.WorkspaceID, applicable, candidates)
	evaluations := make(map[string]core.ObligationEvaluation, len(applicable))
	for _, obligation := range applicable {
		evaluation := EvaluateObligation(obligation, candidates, ProviderAvailability{}, currentEnvironment, quiescence)
		evaluations[obligation.ObligationID] = evaluation
		applyObligationEvaluation(out.Obligations, evaluation)
	}
	out.ObligationViews = projectObligationViews(out.Obligations, evaluations)
	out.Gate, err = GateForPolicyState(state, true, out.Obligations, evaluations)
	if err != nil {
		return Inspection{}, err
	}

	histories := readCostHistories(ctx, sources.Costs, candidates.Candidates)
	out.CostSummary = ProjectBoundRequirementCosts(applicable, candidates, histories)
	return out, nil
}

func applicableObligations(obligations []core.VerificationObligation) []core.VerificationObligation {
	out := make([]core.VerificationObligation, 0, len(obligations))
	for _, obligation := range obligations {
		if obligation.Disposition == core.DispositionRequiredNow || obligation.Disposition == core.DispositionWaived {
			out = append(out, obligation)
		}
	}
	return out
}

func readInspectionCandidates(ctx context.Context, source EvidenceCandidateSource, req InspectRequest) CandidateResultSet {
	if source == nil {
		return CandidateResultSet{Coverage: core.CoverageUnknown, Diagnostics: []string{"evidence_source_unavailable"}}
	}
	result, err := source.Candidates(ctx, CandidateQuery{WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID})
	if err != nil {
		return CandidateResultSet{Coverage: core.CoverageUnknown, Diagnostics: []string{"evidence_source_unavailable"}}
	}
	return result
}

func readCurrentEnvironment(ctx context.Context, source CurrentEnvironmentSource, workspaceID string, obligations []core.VerificationObligation) *environment.Binding {
	needed := false
	for _, obligation := range obligations {
		for _, bound := range obligation.EvidenceRequirements {
			if bound.Requirement.Environment != core.EnvironmentNone {
				needed = true
				break
			}
		}
	}
	if !needed || source == nil {
		return nil
	}
	binding, found, err := source.CurrentBinding(ctx, workspaceID)
	if err != nil || !found || binding.Validate() != nil {
		return nil
	}
	return &binding
}

func readQuiescence(ctx context.Context, source QuiescenceObserver, workspaceID string, obligations []core.VerificationObligation, candidates CandidateResultSet) map[string]core.QuiescenceObservation {
	out := map[string]core.QuiescenceObservation{}
	if source == nil {
		return out
	}
	seen := map[string]bool{}
	for _, candidate := range candidates.Candidates {
		if seen[candidate.OperationID] || !candidateRequiresQuiescence(candidate, obligations) {
			continue
		}
		seen[candidate.OperationID] = true
		observation, found, err := source.Observe(ctx, candidate.OperationID, candidate.SessionID, workspaceID)
		if err == nil && found {
			out[candidate.OperationID] = observation
		}
	}
	return out
}

func candidateRequiresQuiescence(candidate core.EvidenceCandidate, obligations []core.VerificationObligation) bool {
	for _, obligation := range obligations {
		for _, bound := range obligation.EvidenceRequirements {
			if bound.Requirement.RequireQuiescence && candidateRelevantToRequirement(candidate, bound.Requirement, bound.ExpectedProjectBindingDigest) {
				return true
			}
		}
	}
	return false
}

func applyObligationEvaluation(obligations []core.VerificationObligation, evaluation core.ObligationEvaluation) {
	for i := range obligations {
		if obligations[i].ObligationID != evaluation.ObligationID {
			continue
		}
		obligations[i].EvidenceStatus = evaluation.EvidenceStatus
		obligations[i].EvidenceRefs = append([]string(nil), evaluation.EvidenceRefs...)
		return
	}
}

func projectObligationViews(obligations []core.VerificationObligation, evaluations map[string]core.ObligationEvaluation) []ObligationView {
	out := make([]ObligationView, 0, len(obligations))
	for _, obligation := range obligations {
		view := ObligationView{
			ObligationID: obligation.ObligationID, SourceRuleID: obligation.SourceRuleID, Disposition: obligation.Disposition,
			EvidenceStatus: obligation.EvidenceStatus, SufficiencyBasis: obligation.SufficiencyBasis,
			EvidenceRefs: append([]string(nil), obligation.EvidenceRefs...), WaiverID: obligation.WaiverID,
			RequirementResults: []core.RequirementEvaluation{},
		}
		if evaluation, ok := evaluations[obligation.ObligationID]; ok {
			view.RequirementResults = append(view.RequirementResults, evaluation.RequirementResults...)
			view.ReasonCodes = evaluationReasonCodes(evaluation)
		}
		out = append(out, view)
	}
	return out
}

func evaluationReasonCodes(evaluation core.ObligationEvaluation) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, result := range evaluation.RequirementResults {
		if result.ReasonCode == "" || seen[result.ReasonCode] {
			continue
		}
		seen[result.ReasonCode] = true
		out = append(out, result.ReasonCode)
	}
	sort.Strings(out)
	return out
}

func readCostHistories(ctx context.Context, source CostHistorySource, candidates []core.EvidenceCandidate) map[string]CostHistory {
	if source == nil || len(candidates) == 0 {
		return map[string]CostHistory{}
	}
	ids := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.OperationID == "" || seen[candidate.OperationID] {
			continue
		}
		seen[candidate.OperationID] = true
		ids = append(ids, candidate.OperationID)
	}
	sort.Strings(ids)
	histories, err := source.Histories(ctx, ids)
	if err != nil || histories == nil {
		return map[string]CostHistory{}
	}
	return histories
}

func (s *InspectionService) PreviewPolicy(ctx context.Context, req PreviewPolicyRequest) (PolicyPreview, error) {
	if s == nil || s.workspaces == nil || s.loader == nil {
		return PolicyPreview{}, fmt.Errorf("verification policy preview unavailable")
	}
	ws, err := s.workspaces.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return PolicyPreview{}, err
	}
	if req.Profile == "" {
		load := s.loader.Load(ctx, ws)
		return PolicyPreview{State: load.State, Proposal: load.Proposal, RawDigest: load.RawDigest, Code: load.Code}, nil
	}
	if s.projects == nil || s.starters == nil {
		return PolicyPreview{}, fmt.Errorf("verification starter preview unavailable")
	}
	inspection, err := s.projects.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return PolicyPreview{}, err
	}
	preview, err := s.starters.Preview(ctx, req.Profile, string(ws.RepositoryID), inspection.Manifest)
	if err != nil {
		return PolicyPreview{}, err
	}
	preview.State = PolicyLoadValid
	return preview, nil
}

func (s *InspectionService) materializedPolicy(ctx context.Context, repositoryID workspace.RepositoryID) (core.MaterializedPolicy, *core.PolicySummary, bool, error) {
	activation, found, err := s.effective.CurrentActivation(ctx, repositoryID)
	if err != nil || !found {
		return core.MaterializedPolicy{}, nil, false, err
	}
	snapshot, found, err := s.effective.LoadPolicySnapshot(ctx, repositoryID, activation.ProposedPolicyDigest)
	if err != nil {
		return core.MaterializedPolicy{}, nil, false, err
	}
	if !found || snapshot.RepositoryID != string(repositoryID) || snapshot.Digest != activation.ProposedPolicyDigest {
		return core.MaterializedPolicy{}, nil, false, fmt.Errorf("effective verification policy snapshot unavailable")
	}
	digest, err := core.PolicyDigest(snapshot.Content)
	if err != nil || digest != snapshot.Digest {
		return core.MaterializedPolicy{}, nil, false, fmt.Errorf("effective verification policy snapshot invalid")
	}
	materialized := core.MaterializedPolicy{
		Snapshot: snapshot, Source: core.PolicyRepositoryAuthored, ProfileOrigin: activation.ProfileOrigin,
		ApprovalRef: activation.ActivationID, ApprovalAuthority: activation.Authority, ApprovedAt: activation.ActivatedAt,
	}
	summary := core.PolicySummary{
		PolicyID: snapshot.Content.PolicyID, Digest: snapshot.Digest, Source: materialized.Source,
		ProfileOrigin: materialized.ProfileOrigin, ApprovalRef: materialized.ApprovalRef,
		ApprovalAuthority: materialized.ApprovalAuthority, ApprovedAt: materialized.ApprovedAt,
	}
	return materialized, &summary, true, nil
}

func resolvePolicyState(load PolicyLoadResult, effective bool, materialized core.MaterializedPolicy) PolicyState {
	switch load.State {
	case PolicyLoadInvalid:
		return PolicyStateInvalid
	case PolicyLoadUnsupported:
		return PolicyStateUnsupported
	case PolicyLoadValid:
		if load.Proposal == nil {
			return PolicyStateInvalid
		}
		if !effective || load.Proposal.Digest != materialized.Snapshot.Digest {
			return PolicyStateProposalPending
		}
		return PolicyStateEffective
	case PolicyLoadAbsent:
		if effective {
			return PolicyStateEffective
		}
		return PolicyStateAbsent
	default:
		return PolicyStateInvalid
	}
}

func proposalSummary(load PolicyLoadResult, effective bool, materialized core.MaterializedPolicy) *core.PolicyProposalSummary {
	if load.State != PolicyLoadValid || load.Proposal == nil {
		return nil
	}
	if effective && load.Proposal.Digest == materialized.Snapshot.Digest {
		return nil
	}
	proposal := load.Proposal
	return &core.PolicyProposalSummary{PolicyID: proposal.Content.PolicyID, Digest: proposal.Digest, Origin: proposal.Origin, ProfileOrigin: proposal.ProfileOrigin}
}
