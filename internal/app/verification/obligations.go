package verification

import (
	"context"
	"fmt"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

type ObligationRequest struct {
	WorkspaceID   string
	Phase         core.Phase
	Policy        core.MaterializedPolicy
	Surface       core.AffectedSurface
	ActiveWaivers []core.VerificationWaiver
}

type ObligationResult struct {
	Obligations []core.VerificationObligation `json:"obligations"`
	PolicyGaps  []core.PolicyGap              `json:"policy_gaps,omitempty"`
	Diagnostics []string                      `json:"diagnostics,omitempty"`
}

type ObligationMatcher struct{ commands ProjectCommandResolver }

func NewObligationMatcher(commands ProjectCommandResolver) *ObligationMatcher {
	return &ObligationMatcher{commands: commands}
}

type selectorEvaluation struct {
	positive       bool
	provenNonMatch bool
	triggerRefs    []string
	scopeRefs      []string
	uncertain      bool
}

func (m *ObligationMatcher) Derive(ctx context.Context, req ObligationRequest) (ObligationResult, error) {
	if req.Phase.Validate() != nil || req.Surface.Validate() != nil {
		return ObligationResult{}, fmt.Errorf("invalid obligation request")
	}
	digest, err := core.PolicyDigest(req.Policy.Snapshot.Content)
	if err != nil || digest != req.Policy.Snapshot.Digest || req.Policy.Snapshot.RepositoryID != req.Surface.RepositoryID {
		return ObligationResult{}, fmt.Errorf("effective policy binding mismatch")
	}
	if req.Surface.SourceGeneration == "" {
		return ObligationResult{}, fmt.Errorf("source generation unavailable for obligation identity")
	}
	rules := append([]core.Rule(nil), req.Policy.Snapshot.Content.Rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	out := ObligationResult{}
	for _, rule := range rules {
		o, diagnostics, err := m.deriveRule(ctx, req, digest, rule)
		if err != nil {
			return ObligationResult{}, err
		}
		out.Obligations = append(out.Obligations, o)
		out.Diagnostics = append(out.Diagnostics, diagnostics...)
	}
	out.PolicyGaps, err = derivePolicyGaps(req.Surface, req.Policy.Snapshot.Content, digest)
	if err != nil {
		return ObligationResult{}, err
	}
	out.Diagnostics = sortedUniqueStrings(out.Diagnostics)
	return out, nil
}

func (m *ObligationMatcher) deriveRule(ctx context.Context, req ObligationRequest, digest string, rule core.Rule) (core.VerificationObligation, []string, error) {
	selection := evaluateRuleSelectors(req.Surface, digest, rule)
	requiredPhase, disposition := ruleDisposition(rule, req.Phase, selection)
	bound := declaredEvidenceRequirements(rule)
	status := core.EvidenceNotEvaluated
	diagnostics := []string{}
	if disposition != core.DispositionNotTriggered {
		bound, status, diagnostics = m.bindEvidence(ctx, req.WorkspaceID, rule)
	}
	triggerRefs := selection.triggerRefs
	if len(triggerRefs) == 0 {
		triggerRefs = []string{"policy:" + digest, "rule:" + rule.ID}
	}
	scopeRefs := selection.scopeRefs
	if len(scopeRefs) == 0 {
		scopeRefs = surfaceDomainRefs(req.Surface)
	}
	if len(scopeRefs) == 0 {
		scopeRefs = append([]string(nil), triggerRefs...)
	}
	id, err := core.ObligationID(digest, rule.ID, req.Surface.SourceGeneration, triggerRefs)
	if err != nil {
		return core.VerificationObligation{}, nil, err
	}
	o := core.VerificationObligation{SchemaVersion: 1, ObligationID: id, PolicyDigest: digest, SourceRuleID: rule.ID, TriggerRefs: sortedUniqueStrings(triggerRefs), AffectedScopeRefs: sortedUniqueStrings(scopeRefs), Ownership: rule.Ownership, RiskClass: rule.RiskClass, RequiredPhase: requiredPhase, SufficiencyBasis: rule.SufficiencyBasis, MinimumAffectedAuthority: rule.MinimumAffectedAuthority, EvidenceRequirements: bound, AppliesToGeneration: req.Surface.SourceGeneration, Disposition: disposition, EvidenceStatus: status}
	if selection.uncertain && rule.Required && disposition != core.DispositionNotTriggered {
		diagnostics = append(diagnostics, "applicability_uncertain_widened:"+rule.ID)
	}
	if disposition == core.DispositionRequiredNow {
		if waiver := matchingWaiver(req.ActiveWaivers, req.Surface.RepositoryID, digest, rule.ID, req.Phase, req.Surface.SourceGeneration); waiver != nil {
			o.Disposition = core.DispositionWaived
			o.WaiverID = waiver.WaiverID
		}
	}
	if err := o.Validate(); err != nil {
		return core.VerificationObligation{}, nil, err
	}
	return o, diagnostics, nil
}

func declaredEvidenceRequirements(rule core.Rule) []core.BoundEvidenceRequirement {
	requirements := append([]core.EvidenceRequirement(nil), rule.Evidence...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	out := make([]core.BoundEvidenceRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		out = append(out, core.BoundEvidenceRequirement{Requirement: requirement})
	}
	return out
}

func (m *ObligationMatcher) bindEvidence(ctx context.Context, workspaceID string, rule core.Rule) ([]core.BoundEvidenceRequirement, core.EvidenceStatus, []string) {
	requirements := append([]core.EvidenceRequirement(nil), rule.Evidence...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	out := make([]core.BoundEvidenceRequirement, 0, len(requirements))
	status := core.EvidenceNotEvaluated
	diagnostics := []string{}
	for _, requirement := range requirements {
		bound := core.BoundEvidenceRequirement{Requirement: requirement}
		if requirement.ProjectCommandID != "" {
			if m == nil || m.commands == nil {
				status = core.EvidenceUnavailable
				diagnostics = append(diagnostics, "project_binding_unavailable:"+rule.ID+":"+requirement.ID)
			} else if binding, err := m.commands.Resolve(ctx, workspaceID, requirement.ProjectCommandID, requirement.Params); err != nil {
				status = core.EvidenceUnavailable
				diagnostics = append(diagnostics, "project_binding_unavailable:"+rule.ID+":"+requirement.ID)
			} else if digest, err := binding.Digest(); err != nil {
				status = core.EvidenceUnavailable
				diagnostics = append(diagnostics, "project_binding_invalid:"+rule.ID+":"+requirement.ID)
			} else {
				bound.ExpectedProjectBindingDigest = digest
			}
		}
		out = append(out, bound)
	}
	return out, status, diagnostics
}

func evaluateRuleSelectors(surface core.AffectedSurface, policyDigest string, rule core.Rule) selectorEvaluation {
	if len(rule.MatchPaths) == 0 && len(rule.MatchClasses) == 0 {
		return selectorEvaluation{positive: true, triggerRefs: []string{"policy:" + policyDigest, "rule:" + rule.ID}, scopeRefs: surfaceDomainRefs(surface)}
	}
	out := selectorEvaluation{}
	for _, relation := range surface.Relations {
		if !core.MeetsMinimumAuthority(relation.DerivationAuthority, rule.MinimumAffectedAuthority) {
			continue
		}
		if relationMatchesPath(relation, rule.MatchPaths) || relationMatchesClass(relation, policyDigest, rule.MatchClasses) {
			out.positive = true
			out.triggerRefs = append(out.triggerRefs, relation.RelationID)
		}
	}
	out.scopeRefs = surfaceDomainRefs(surface)
	if out.positive {
		return out
	}
	pathProven := len(rule.MatchPaths) == 0 || strongPathNonMatchDomain(surface, rule.MinimumAffectedAuthority)
	classProven := len(rule.MatchClasses) == 0 || strongClassNonMatchDomain(surface, policyDigest, rule.MinimumAffectedAuthority)
	out.provenNonMatch = pathProven && classProven
	out.uncertain = !out.provenNonMatch
	return out
}

func relationMatchesPath(r core.AffectedRelation, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	return (r.From.Kind == core.SubjectPath && matchesPolicyPath(patterns, r.From.Value)) || (r.To.Kind == core.SubjectPath && matchesPolicyPath(patterns, r.To.Value))
}

func relationMatchesClass(r core.AffectedRelation, digest string, classes []string) bool {
	if len(classes) == 0 || r.Kind != "classified_as" || r.To.Kind != core.SubjectSurfaceClass || !containsString(classes, r.To.Value) {
		return false
	}
	return containsString(r.ProvenanceRefs, "policy:"+digest)
}

func strongPathNonMatchDomain(surface core.AffectedSurface, minimum core.DerivationAuthority) bool {
	if !strongDomain(surface, core.DomainSourceSelection, "", minimum) {
		return false
	}
	for _, domain := range surface.Domains {
		if domain.Kind == core.DomainGoImportGraph && (domain.Coverage != core.CoverageComplete || !core.MeetsMinimumAuthority(domain.DerivationAuthority, minimum)) {
			return false
		}
	}
	return true
}

func strongClassNonMatchDomain(surface core.AffectedSurface, digest string, minimum core.DerivationAuthority) bool {
	return strongDomain(surface, core.DomainPolicyClassification, "policy:"+digest, minimum)
}

func strongDomain(surface core.AffectedSurface, kind core.AffectedDomainKind, provenance string, minimum core.DerivationAuthority) bool {
	for _, domain := range surface.Domains {
		if domain.Kind != kind || domain.Coverage != core.CoverageComplete || !core.MeetsMinimumAuthority(domain.DerivationAuthority, minimum) {
			continue
		}
		if provenance == "" || containsString(domain.ProvenanceRefs, provenance) {
			return true
		}
	}
	return false
}

func ruleDisposition(rule core.Rule, current core.Phase, selection selectorEvaluation) (core.Phase, core.ObligationDisposition) {
	requiredPhase := selectedRequiredPhase(rule.Phases, current)
	if selection.provenNonMatch {
		return requiredPhase, core.DispositionNotTriggered
	}
	if !rule.Required {
		if containsPhase(rule.Phases, current) {
			return current, core.DispositionOptional
		}
		return requiredPhase, core.DispositionNotTriggered
	}
	if containsPhase(rule.Phases, current) {
		return current, core.DispositionRequiredNow
	}
	if later, ok := nextPipelinePhase(rule.Phases, current); ok {
		return later, core.DispositionDeferred
	}
	return requiredPhase, core.DispositionNotTriggered
}

func selectedRequiredPhase(phases []core.Phase, current core.Phase) core.Phase {
	if containsPhase(phases, current) {
		return current
	}
	if next, ok := nextPipelinePhase(phases, current); ok {
		return next
	}
	ordered := append([]core.Phase(nil), phases...)
	sort.Slice(ordered, func(i, j int) bool { return phaseSortKey(ordered[i]) < phaseSortKey(ordered[j]) })
	if len(ordered) == 0 {
		return current
	}
	return ordered[0]
}

func nextPipelinePhase(phases []core.Phase, current core.Phase) (core.Phase, bool) {
	currentRank, ok := pipelinePhaseRank(current)
	if !ok {
		return "", false
	}
	bestRank := 99
	var best core.Phase
	for _, phase := range phases {
		rank, ok := pipelinePhaseRank(phase)
		if !ok || rank <= currentRank || rank >= bestRank {
			continue
		}
		best, bestRank = phase, rank
	}
	return best, best != ""
}

func pipelinePhaseRank(phase core.Phase) (int, bool) {
	switch phase {
	case core.PhaseInnerLoop:
		return 0, true
	case core.PhaseCheckpoint:
		return 1, true
	case core.PhasePreMerge:
		return 2, true
	case core.PhaseRelease:
		return 3, true
	}
	return 0, false
}

func phaseSortKey(phase core.Phase) int {
	if rank, ok := pipelinePhaseRank(phase); ok {
		return rank
	}
	if phase == core.PhaseNightly {
		return 10
	}
	if phase == core.PhasePeriodic {
		return 11
	}
	return 99
}

func matchingWaiver(waivers []core.VerificationWaiver, repositoryID, digest, ruleID string, phase core.Phase, generation string) *core.VerificationWaiver {
	ordered := append([]core.VerificationWaiver(nil), waivers...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].WaiverID < ordered[j].WaiverID })
	for i := range ordered {
		w := &ordered[i]
		if w.RepositoryID == repositoryID && w.PolicyDigest == digest && w.RuleID == ruleID && w.Phase == phase && (w.Generation == "" || w.Generation == generation) {
			return w
		}
	}
	return nil
}

func derivePolicyGaps(surface core.AffectedSurface, policy core.PolicyContent, digest string) ([]core.PolicyGap, error) {
	coveredClasses := map[string]bool{}
	for _, rule := range policy.Rules {
		for _, class := range rule.MatchClasses {
			coveredClasses[class] = true
		}
	}
	gaps := []core.PolicyGap{}
	for _, relation := range surface.Relations {
		if relation.Kind != "classified_as" || relation.From.Kind != core.SubjectPath || relation.To.Kind != core.SubjectSurfaceClass || !core.MeetsMinimumAuthority(relation.DerivationAuthority, core.AuthorityMechanical) || !containsString(relation.ProvenanceRefs, "policy:"+digest) || coveredClasses[relation.To.Value] {
			continue
		}
		classSource := classificationSource(relation.ProvenanceRefs)
		if classSource == "" {
			continue
		}
		id, err := core.PolicyGapID(digest, classSource, surface.SourceGeneration, []string{relation.RelationID})
		if err != nil {
			return nil, err
		}
		gap := core.PolicyGap{GapID: id, SurfaceRef: relation.From.Value, DeclaredClass: relation.To.Value, ClassificationSource: classSource, MissingPolicyClass: relation.To.Value, Authority: relation.DerivationAuthority, ProvenanceRefs: append([]string(nil), relation.ProvenanceRefs...)}
		if err := gap.Validate(); err != nil {
			return nil, err
		}
		gaps = append(gaps, gap)
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].SurfaceRef == gaps[j].SurfaceRef {
			return gaps[i].DeclaredClass < gaps[j].DeclaredClass
		}
		return gaps[i].SurfaceRef < gaps[j].SurfaceRef
	})
	return gaps, nil
}

func classificationSource(refs []string) string {
	for _, ref := range refs {
		if strings.HasPrefix(ref, "classification:") {
			return strings.TrimPrefix(ref, "classification:")
		}
	}
	return ""
}

func surfaceDomainRefs(surface core.AffectedSurface) []string {
	out := make([]string, 0, len(surface.Domains))
	for _, domain := range surface.Domains {
		if domain.DomainID != "" {
			out = append(out, domain.DomainID)
		}
	}
	return sortedUniqueStrings(out)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
