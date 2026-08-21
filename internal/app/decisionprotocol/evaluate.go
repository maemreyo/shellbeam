package decisionprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func EvaluateRequirements(policy core.PolicyContent, facts core.EvaluationFacts) core.DecisionProtocolEvaluation {
	evaluations := make([]core.DecisionRequirementEvaluation, 0, len(policy.Requirements))
	for _, requirement := range policy.Requirements {
		evaluations = append(evaluations, evaluateRequirement(requirement, facts))
	}
	sort.Slice(evaluations, func(i, j int) bool { return evaluations[i].RequirementID < evaluations[j].RequirementID })
	blockers := requiredPredictionBlockers(facts)
	gate := FoldGate(evaluations, blockers)
	digest, err := core.BlockingRequirementDigest(evaluations, blockers)
	if err != nil {
		digest = "block_" + strings.Repeat("0", 64)
		gate.Status = core.GateIndeterminate
	}
	return core.DecisionProtocolEvaluation{EpisodeID: facts.EpisodeID, CandidateID: facts.CandidateID, RequirementEvaluations: evaluations, CandidateContractBlockers: blockers, Gate: gate.Status, BlockingRequirementDigest: digest}
}

func evaluateRequirement(requirement core.DecisionRequirement, facts core.EvaluationFacts) core.DecisionRequirementEvaluation {
	switch requirement.Kind {
	case core.RequirementCandidateChallenge:
		return evaluateCandidateChallenge(requirement, facts)
	case core.RequirementPredictionEvaluation:
		return evaluatePredictionRequirement(requirement, facts)
	case core.RequirementDiscrimination:
		return evaluateDiscriminationRequirement(requirement, facts)
	case core.RequirementVerifierAssessment:
		return evaluateVerifierRequirement(requirement, facts)
	default:
		return requirementEvaluation(requirement, core.RequirementIndeterminate, "unsupported_requirement_kind", nil)
	}
}

func evaluateCandidateChallenge(requirement core.DecisionRequirement, facts core.EvaluationFacts) core.DecisionRequirementEvaluation {
	roots := map[core.CandidateID]bool{}
	refs := []string{}
	for _, candidate := range facts.Candidates {
		if candidate.LineageRoot == "" {
			continue
		}
		roots[candidate.LineageRoot] = true
	}
	for root := range roots {
		refs = append(refs, "candidate_lineage:"+string(root))
	}
	if uint64(len(roots)) >= requirement.CandidateChallenge.MinimumDistinctLineages {
		return requirementEvaluation(requirement, core.RequirementSatisfied, "candidate_challenge_satisfied", refs)
	}
	return requirementEvaluation(requirement, core.RequirementUnsatisfied, "candidate_challenge_insufficient", refs)
}

func evaluatePredictionRequirement(requirement core.DecisionRequirement, facts core.EvaluationFacts) core.DecisionRequirementEvaluation {
	roles := map[core.PredictionRole]bool{}
	for _, role := range requirement.PredictionEvaluation.Roles {
		roles[role] = true
	}
	count := uint64(0)
	refs := []string{}
	for _, prediction := range facts.Predictions {
		if prediction.CandidateID != facts.CandidateID || !roles[prediction.Role] || !prediction.Sealed || !prediction.Linked {
			continue
		}
		if prediction.Status != core.PredictionMatch && prediction.Status != core.PredictionMismatch {
			continue
		}
		count++
		refs = append(refs, "prediction:"+string(prediction.PredictionID))
	}
	if count >= requirement.PredictionEvaluation.MinimumEvaluatedPredictions {
		return requirementEvaluation(requirement, core.RequirementSatisfied, "prediction_evaluation_satisfied", refs)
	}
	return requirementEvaluation(requirement, core.RequirementUnsatisfied, "prediction_evaluation_insufficient", refs)
}

func evaluateDiscriminationRequirement(requirement core.DecisionRequirement, facts core.EvaluationFacts) core.DecisionRequirementEvaluation {
	qualified, indeterminate := uint64(0), uint64(0)
	refs := []string{}
	for _, experiment := range facts.Experiments {
		if experiment.CandidateID != facts.CandidateID || !experiment.Potential || !experiment.Linked || !experiment.ObservationSettled || experiment.Aborted || !experiment.Closed {
			continue
		}
		refs = append(refs, "experiment:"+string(experiment.ExperimentID))
		if requirement.Discrimination.RequiredOutcome == core.DiscriminationAttempted {
			qualified++
			continue
		}
		switch experiment.Realized {
		case core.DiscriminationResultPartitioned:
			qualified++
		case core.DiscriminationResultUnavailable:
			indeterminate++
		}
	}
	minimum := requirement.Discrimination.MinimumQualifyingExperiments
	if qualified >= minimum {
		return requirementEvaluation(requirement, core.RequirementSatisfied, "discrimination_satisfied", refs)
	}
	if requirement.Discrimination.RequiredOutcome == core.DiscriminationRealized && qualified+indeterminate >= minimum {
		return requirementEvaluation(requirement, core.RequirementIndeterminate, "discrimination_realization_unavailable", refs)
	}
	return requirementEvaluation(requirement, core.RequirementUnsatisfied, "discrimination_insufficient", refs)
}

func evaluateVerifierRequirement(requirement core.DecisionRequirement, facts core.EvaluationFacts) core.DecisionRequirementEvaluation {
	qualified, unresolved := uint64(0), uint64(0)
	refs := []string{}
	for _, assessment := range facts.VerifierAssessments {
		if !containsCandidate(assessment.PreferredCandidates, facts.CandidateID) {
			continue
		}
		if requirement.VerifierAssessment.RequiredContextClass == "" {
			qualified++
			refs = append(refs, "assessment:"+assessment.AssessmentID)
			continue
		}
		if assessment.QualifiedContextClass == requirement.VerifierAssessment.RequiredContextClass {
			qualified++
			refs = append(refs, "assessment:"+assessment.AssessmentID)
		} else if assessment.QualifiedContextClass == "" {
			unresolved++
		}
	}
	minimum := requirement.VerifierAssessment.MinimumSupportingAssessments
	if qualified >= minimum {
		return requirementEvaluation(requirement, core.RequirementSatisfied, "verifier_assessment_satisfied", refs)
	}
	if qualified+unresolved >= minimum {
		return requirementEvaluation(requirement, core.RequirementIndeterminate, "verifier_context_unresolved", refs)
	}
	return requirementEvaluation(requirement, core.RequirementUnsatisfied, "verifier_assessment_insufficient", refs)
}

func requiredPredictionBlockers(facts core.EvaluationFacts) []core.CandidateContractBlocker {
	blockers := []core.CandidateContractBlocker{}
	for _, prediction := range facts.Predictions {
		if prediction.CandidateID != facts.CandidateID || prediction.Role != core.PredictionRequired || prediction.Status != core.PredictionMismatch {
			continue
		}
		blockers = append(blockers, core.CandidateContractBlocker{Code: core.CandidateBlockerRequiredPredictionMismatch, PredictionID: prediction.PredictionID, BasisRefs: append([]string(nil), prediction.BasisRefs...)})
	}
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].PredictionID < blockers[j].PredictionID })
	return blockers
}

func FoldGate(reqs []core.DecisionRequirementEvaluation, blockers []core.CandidateContractBlocker) core.DecisionProtocolGate {
	if len(blockers) > 0 {
		return core.DecisionProtocolGate{Status: core.GateBlocked}
	}
	indeterminate := false
	for _, requirement := range reqs {
		if requirement.Status == core.RequirementUnsatisfied {
			return core.DecisionProtocolGate{Status: core.GateBlocked}
		}
		if requirement.Status == core.RequirementIndeterminate {
			indeterminate = true
		}
	}
	if indeterminate {
		return core.DecisionProtocolGate{Status: core.GateIndeterminate}
	}
	return core.DecisionProtocolGate{Status: core.GateClear}
}

func EvaluateBudget(budget core.DecisionBudget, usage core.BudgetUsage) core.BudgetAdmission {
	out := core.BudgetAdmission{MayStartExperiment: true, MayLinkOperation: true, MachineWallQuality: usage.MachineWallQuality}
	if out.MachineWallQuality == "" {
		out.MachineWallQuality = core.MachineWallNotObserved
	}
	if budget.MaxExperimentsStarted != nil && usage.ExperimentsStarted >= *budget.MaxExperimentsStarted {
		out.ExperimentsExhausted = true
		out.MayStartExperiment = false
	}
	if budget.MaxLinkedOperations != nil && usage.LinkedOperations >= *budget.MaxLinkedOperations {
		out.LinksExhausted = true
		out.MayLinkOperation = false
	}
	if budget.MaxMachineWallMS != nil && usage.MachineWallQuality != core.MachineWallNotObserved && usage.MachineWallMS >= *budget.MaxMachineWallMS {
		out.MachineWallExhausted = true
		out.MayStartExperiment = false
		out.MayLinkOperation = false
	}
	return out
}

func requirementEvaluation(requirement core.DecisionRequirement, status core.DecisionRequirementStatus, reason string, refs []string) core.DecisionRequirementEvaluation {
	sorted := append([]string(nil), refs...)
	sort.Strings(sorted)
	return core.DecisionRequirementEvaluation{RequirementID: requirement.RequirementID, Kind: requirement.Kind, Status: status, ReasonCode: reason, BasisRefs: sorted}
}

func containsCandidate(values []core.CandidateID, target core.CandidateID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type serviceEvaluationContext struct {
	episode       core.Episode
	policy        core.PolicySnapshot
	records       []core.CanonicalRecordEnvelope
	view          core.DecisionProjection
	facts         core.EvaluationFacts
	budgetUsage   core.BudgetUsage
	verifierState []core.VerifierSemanticState
}

func (s *Service) Evaluate(ctx context.Context, episodeID core.EpisodeID, candidateID core.CandidateID) (core.DecisionProtocolEvaluation, error) {
	loaded, err := s.loadServiceEvaluationContext(ctx, episodeID, candidateID)
	if err != nil {
		return core.DecisionProtocolEvaluation{}, err
	}
	return EvaluateRequirements(loaded.policy.Content, loaded.facts), nil
}

func (s *Service) Project(ctx context.Context, episodeID core.EpisodeID, candidateID core.CandidateID) (core.DecisionProjection, error) {
	loaded, err := s.loadServiceEvaluationContext(ctx, episodeID, candidateID)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	evaluation := EvaluateRequirements(loaded.policy.Content, loaded.facts)
	budget := EvaluateBudget(loaded.policy.Content.Budget, loaded.budgetUsage)
	view := loaded.view
	view.Protocol = evaluation
	view.Budget = budget
	view.AllowedProtocolTransitions = allowedProtocolTransitions(view, loaded.policy.Content, budget)
	semantic := semanticProjectionState(view, loaded.policy, loaded.facts, loaded.verifierState)
	view.ProjectionDigest, err = core.ProjectionDigest(semantic)
	if err != nil {
		return core.DecisionProjection{}, err
	}
	seqs := make([]core.RecordSeq, 0, len(loaded.records))
	basis := []string{}
	for _, record := range loaded.records {
		seqs = append(seqs, record.CanonicalRecordSeq)
	}
	for _, requirement := range evaluation.RequirementEvaluations {
		basis = append(basis, requirement.BasisRefs...)
	}
	for _, blocker := range evaluation.CandidateContractBlockers {
		basis = append(basis, blocker.BasisRefs...)
	}
	basis = uniqueSortedStrings(basis)
	view.AuditDigest, err = core.AuditDigest(core.AuditState{EpisodeID: episodeID, CanonicalRecordSeqs: seqs, BasisRefs: basis})
	if err != nil {
		return core.DecisionProjection{}, err
	}
	return view, nil
}

func (s *Service) loadServiceEvaluationContext(ctx context.Context, episodeID core.EpisodeID, candidateID core.CandidateID) (serviceEvaluationContext, error) {
	if s == nil || s.policies == nil || s.mutations == nil || s.ledger == nil || s.workspaces == nil || s.snapshots == nil {
		return serviceEvaluationContext{}, fmt.Errorf("decision evaluation dependencies unavailable")
	}
	episode, found, err := s.mutations.FindEpisode(ctx, episodeID)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	if !found {
		return serviceEvaluationContext{}, ErrEpisodeNotFound
	}
	policy, found, err := s.policies.LoadPolicySnapshot(ctx, episode.RepositoryID, episode.PolicyBinding.PolicyDigest)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	if !found || policy.PolicyDigest != episode.PolicyBinding.PolicyDigest {
		return serviceEvaluationContext{}, fmt.Errorf("episode-bound policy snapshot unavailable")
	}
	hw, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, episodeID, hw)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	view, err := projectEpisodeRecords(episode, candidateID, records)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	compatibility := s.sourceCompatibility(ctx, episode)
	view.SourceGenerationCompatibility = compatibility
	view.SourceCompatible = compatibility == core.SourceGenerationCurrent
	facts, usage, verifierState, err := evaluationFactsFromRecords(candidateID, view, records)
	if err != nil {
		return serviceEvaluationContext{}, err
	}
	facts.EpisodeID = episodeID
	return serviceEvaluationContext{episode: episode, policy: policy, records: records, view: view, facts: facts, budgetUsage: usage, verifierState: verifierState}, nil
}

func evaluationFactsFromRecords(candidateID core.CandidateID, view core.DecisionProjection, records []core.CanonicalRecordEnvelope) (core.EvaluationFacts, core.BudgetUsage, []core.VerifierSemanticState, error) {
	facts := core.EvaluationFacts{CandidateID: candidateID}
	for _, candidate := range view.Candidates {
		facts.Candidates = append(facts.Candidates, core.CandidateEvaluationFact{CandidateID: candidate.CandidateID, LineageRoot: candidate.LineageRoot})
	}
	seals := map[core.ExperimentID]bool{}
	links := map[core.ExperimentID]bool{}
	results := map[core.PredictionID]core.PredictionResult{}
	usage := core.BudgetUsage{MachineWallQuality: core.MachineWallNotObserved}
	verifierState := []core.VerifierSemanticState{}
	predictions := []core.PredictionBinding{}
	for _, record := range records {
		switch record.Kind {
		case core.RecordExperiment:
			usage.ExperimentsStarted++
		case core.RecordExperimentSeal:
			var seal core.ExperimentSeal
			if err := json.Unmarshal(record.Body, &seal); err != nil {
				return facts, usage, nil, err
			}
			seals[seal.ExperimentID] = true
		case core.RecordExperimentExecutionLink:
			var link core.ExperimentExecutionLink
			if err := json.Unmarshal(record.Body, &link); err != nil {
				return facts, usage, nil, err
			}
			links[link.ExperimentID] = true
			usage.LinkedOperations++
		case core.RecordPredictionBinding:
			var prediction core.PredictionBinding
			if err := json.Unmarshal(record.Body, &prediction); err != nil {
				return facts, usage, nil, err
			}
			predictions = append(predictions, prediction)
		case core.RecordExperimentObservationBinding:
			var binding core.ExperimentObservationBinding
			if err := json.Unmarshal(record.Body, &binding); err != nil {
				return facts, usage, nil, err
			}
			for _, result := range binding.PredictionResults {
				results[result.PredictionID] = result
			}
		case core.RecordVerifierAssessment:
			var assessment core.VerifierAssessment
			if err := json.Unmarshal(record.Body, &assessment); err != nil {
				return facts, usage, nil, err
			}
			facts.VerifierAssessments = append(facts.VerifierAssessments, assessment)
			verifierState = append(verifierState, core.VerifierSemanticState{ActorRef: assessment.ActorRef, QualifiedContextClass: assessment.QualifiedContextClass, PreferredCandidates: append([]core.CandidateID(nil), assessment.PreferredCandidates...), SemanticRejections: append([]core.CandidateID(nil), assessment.SemanticRejections...)})
		}
	}
	for _, prediction := range predictions {
		status := core.PredictionNotEvaluated
		basis := []string(nil)
		if result, ok := results[prediction.PredictionID]; ok {
			status, basis = result.Status, append([]string(nil), result.BasisRefs...)
		}
		facts.Predictions = append(facts.Predictions, core.PredictionEvaluationFact{PredictionID: prediction.PredictionID, CandidateID: prediction.CandidateID, Role: prediction.Role, Sealed: seals[prediction.ExperimentID], Linked: links[prediction.ExperimentID], Status: status, BasisRefs: basis})
	}
	for _, experiment := range view.Experiments {
		potential := false
		for _, pair := range experiment.PotentialDiscrimination {
			if pair.TargetCandidateID == candidateID {
				potential = true
				break
			}
		}
		realized := core.DiscriminationResultUnavailable
		if experiment.ObservationState == core.ObservationSettled {
			realized = core.DiscriminationResultNoPartition
			if experiment.RealizedDiscrimination {
				realized = core.DiscriminationResultPartitioned
			}
		}
		facts.Experiments = append(facts.Experiments, core.DiscriminationEvaluationFact{ExperimentID: experiment.ExperimentID, CandidateID: candidateID, Potential: potential, Linked: experiment.ObservationState != "", ObservationSettled: experiment.ObservationState == core.ObservationSettled, Closed: experiment.State == core.ExperimentClosed, Aborted: experiment.State == core.ExperimentAborted, Realized: realized})
	}
	return facts, usage, verifierState, nil
}

func semanticProjectionState(view core.DecisionProjection, policy core.PolicySnapshot, facts core.EvaluationFacts, verifier []core.VerifierSemanticState) core.ProjectionSemanticState {
	return core.ProjectionSemanticState{
		EpisodeID: view.EpisodeID, EpisodeState: view.EpisodeState, PolicyDigest: policy.PolicyDigest, CandidateID: view.CandidateID,
		CandidateStates: candidateSemanticStatesWithFacts(view.Candidates, facts.Predictions), ExperimentStates: experimentSemanticStates(view.Experiments),
		RequirementStates: requirementSemanticStates(view.Protocol.RequirementEvaluations), Gate: view.Protocol.Gate, VerifierState: verifier,
		SourceCompatible: view.SourceCompatible, Budget: view.Budget, AllowedProtocolTransitions: append([]string(nil), view.AllowedProtocolTransitions...),
	}
}

func candidateSemanticStatesWithFacts(candidates []core.CandidateProjection, predictions []core.PredictionEvaluationFact) []core.CandidateSemanticState {
	out := candidateSemanticStates(candidates)
	for i := range out {
		blocked := false
		for _, prediction := range predictions {
			if prediction.CandidateID != out[i].CandidateID {
				continue
			}
			out[i].ExpectationOutcomes = append(out[i].ExpectationOutcomes, prediction.Status)
			if prediction.Role == core.PredictionRequired && prediction.Status == core.PredictionMismatch {
				blocked = true
			}
		}
		out[i].Eligible = out[i].Active && !blocked
	}
	return out
}

func experimentSemanticStates(experiments []core.ExperimentProjection) []core.ExperimentSemanticState {
	out := make([]core.ExperimentSemanticState, 0, len(experiments))
	for _, experiment := range experiments {
		out = append(out, core.ExperimentSemanticState{ExperimentID: experiment.ExperimentID, State: experiment.State, ObservationState: experiment.ObservationState, PotentialDiscrimination: append([]core.PotentialDiscriminationPair(nil), experiment.PotentialDiscrimination...), RealizedDiscrimination: experiment.RealizedDiscrimination})
	}
	return out
}

func requirementSemanticStates(values []core.DecisionRequirementEvaluation) []core.RequirementSemanticState {
	out := make([]core.RequirementSemanticState, 0, len(values))
	for _, value := range values {
		out = append(out, core.RequirementSemanticState{RequirementID: value.RequirementID, Kind: value.Kind, Status: value.Status})
	}
	return out
}

func allowedProtocolTransitions(view core.DecisionProjection, policy core.PolicyContent, budget core.BudgetAdmission) []string {
	if view.EpisodeState != core.EpisodeOpen {
		return nil
	}
	allowed := []string{"decision.candidate.create", "decision.assessment.record", "decision.close_unresolved"}
	if budget.MayStartExperiment {
		allowed = append(allowed, "decision.experiment.define")
	}
	if view.CandidateID != "" {
		allowed = append(allowed, "decision.selection.propose")
		if candidateActive(view.Candidates, view.CandidateID) {
			allowed = append(allowed, "decision.candidate.revise")
		}
		if view.Protocol.Gate == core.GateClear && view.SourceCompatible && candidateActive(view.Candidates, view.CandidateID) {
			allowed = append(allowed, "decision.selection.commit")
		}
		if view.Protocol.Gate != core.GateClear && policy.OverridePolicy.Allowed {
			allowed = append(allowed, "decision.override.create")
		}
	}
	for _, experiment := range view.Experiments {
		switch experiment.State {
		case core.ExperimentDefined:
			allowed = append(allowed, "decision.prediction.bind", "decision.experiment.seal", "decision.experiment.abort")
		case core.ExperimentSealed:
			allowed = append(allowed, "decision.experiment.abort")
		case core.ExperimentObserving:
			allowed = append(allowed, "decision.experiment.close", "decision.experiment.abort")
		}
	}
	return uniqueSortedStrings(allowed)
}

func candidateActive(values []core.CandidateProjection, id core.CandidateID) bool {
	for _, value := range values {
		if value.CandidateID == id {
			return value.State == core.CandidateActive
		}
	}
	return false
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
