package decisionprotocol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

const observationSemanticsVersion = 1

type observationMaterializationPlan struct {
	existing    *core.ExperimentObservationBinding
	link        core.ExperimentExecutionLink
	predictions []core.PredictionBinding
}

func (s *Service) materializeExperimentObservation(ctx context.Context, experimentID core.ExperimentID) (core.ExperimentObservationBinding, error) {
	plan, err := s.loadObservationMaterializationPlan(ctx, experimentID)
	if err != nil {
		return core.ExperimentObservationBinding{}, err
	}
	if plan.existing != nil {
		return *plan.existing, nil
	}
	rec, err := s.loadObservationReceipt(ctx, plan.link)
	if err != nil {
		return core.ExperimentObservationBinding{}, err
	}
	observation, structured, verification, err := s.loadPredicateObservation(ctx, plan.link, plan.predictions, rec)
	if err != nil {
		return core.ExperimentObservationBinding{}, err
	}
	results := evaluatePredictionResults(plan.predictions, observation)
	cutDigest, err := deriveObservationCutDigest(rec, structured, verification)
	if err != nil {
		return core.ExperimentObservationBinding{}, err
	}
	binding := core.ExperimentObservationBinding{
		SchemaVersion: 1, BindingID: core.BindingID(semanticRecordID("bind", string(experimentID), cutDigest)), ExperimentID: experimentID,
		OperationID: plan.link.OperationID, SourceGeneration: plan.link.SourceGeneration, ObservationSemanticsVersion: observationSemanticsVersion,
		DerivationCutDigest: cutDigest, PredictionResults: results, MaterializedAt: s.now().UTC(),
	}
	stored, _, err := s.experiments.MaterializeExperimentObservationCAS(ctx, binding)
	if err != nil {
		return core.ExperimentObservationBinding{}, err
	}
	return stored, nil
}

func (s *Service) loadObservationMaterializationPlan(ctx context.Context, experimentID core.ExperimentID) (observationMaterializationPlan, error) {
	if s == nil || s.experiments == nil || s.ledger == nil {
		return observationMaterializationPlan{}, fmt.Errorf("decision observation dependencies unavailable")
	}
	experiment, ok, err := s.experiments.FindExperiment(ctx, experimentID)
	if err != nil {
		return observationMaterializationPlan{}, err
	}
	if !ok {
		return observationMaterializationPlan{}, ErrExperimentNotFound
	}
	hw, err := s.ledger.CurrentHighWater(ctx)
	if err != nil {
		return observationMaterializationPlan{}, err
	}
	records, err := s.ledger.ListEpisodeRecords(ctx, experiment.EpisodeID, hw)
	if err != nil {
		return observationMaterializationPlan{}, err
	}
	if existing, found, err := uniqueObservationBinding(records, experimentID); err != nil {
		return observationMaterializationPlan{}, err
	} else if found {
		return observationMaterializationPlan{existing: &existing}, nil
	}
	seal, found, err := existingExperimentSeal(records, experimentID)
	if err != nil {
		return observationMaterializationPlan{}, err
	}
	if !found {
		return observationMaterializationPlan{}, core.NewReasonError(core.ReasonObservationNotSettled, "experiment is not sealed")
	}
	links := executionLinks(records, experimentID)
	if len(links) != 1 {
		return observationMaterializationPlan{}, core.NewReasonError(core.ReasonObservationNotSettled, "experiment observation requires one linked execution")
	}
	predictions := predictionsForExperiment(records, experimentID)
	if len(predictions) == 0 {
		return observationMaterializationPlan{}, fmt.Errorf("sealed experiment has no predictions")
	}
	digest, err := core.PredictionSetDigest(predictions)
	if err != nil {
		return observationMaterializationPlan{}, err
	}
	if digest != seal.SealedPredictionDigest {
		return observationMaterializationPlan{}, fmt.Errorf("sealed prediction set changed")
	}
	return observationMaterializationPlan{link: links[0], predictions: predictions}, nil
}

func (s *Service) loadObservationReceipt(ctx context.Context, link core.ExperimentExecutionLink) (receipt.Receipt, error) {
	if s.receipts == nil {
		return receipt.Receipt{}, core.NewReasonError(core.ReasonObservationNotSettled, "receipt source unavailable")
	}
	rec, found, err := s.receipts.FindReceiptByOperation(ctx, operation.ID(link.OperationID))
	if err != nil {
		return receipt.Receipt{}, err
	}
	if !found || !rec.State.Terminal() {
		return receipt.Receipt{}, core.NewReasonError(core.ReasonObservationNotSettled, "linked operation receipt not terminal")
	}
	if rec.OperationID != string(link.OperationID) || rec.SessionID != string(link.SessionID) {
		return receipt.Receipt{}, fmt.Errorf("linked operation receipt authority mismatch")
	}
	return rec, nil
}

func (s *Service) loadPredicateObservation(ctx context.Context, link core.ExperimentExecutionLink, predictions []core.PredictionBinding, rec receipt.Receipt) (PredicateObservation, *structuredapp.InspectResult, *QualifiedEvidenceSet, error) {
	observation := PredicateObservation{OperationID: string(link.OperationID), SourceGeneration: link.SourceGeneration, Receipt: &rec}
	structured, err := s.loadStructuredObservation(ctx, link, predictions)
	if err != nil {
		return PredicateObservation{}, nil, nil, err
	}
	if structured != nil {
		observation.Structured = structured
	}
	verification, err := s.loadVerificationObservation(ctx, link, predictions)
	if err != nil {
		return PredicateObservation{}, nil, nil, err
	}
	if verification != nil {
		observation.Verification = verification
	}
	return observation, structured, verification, nil
}

func (s *Service) loadStructuredObservation(ctx context.Context, link core.ExperimentExecutionLink, predictions []core.PredictionBinding) (*structuredapp.InspectResult, error) {
	if !predictionsNeedStructured(predictions) {
		return nil, nil
	}
	if s.structured == nil {
		return nil, core.NewReasonError(core.ReasonObservationNotSettled, "structured source unavailable")
	}
	result, err := s.structured.InspectStructured(ctx, structuredapp.InspectRequest{OperationID: string(link.OperationID), MaxRecords: structuredapp.MaxListRecords})
	if err != nil {
		return nil, err
	}
	if result.Status == structuredapp.InspectPending || result.Status == structuredapp.InspectProcessing {
		return nil, core.NewReasonError(core.ReasonObservationNotSettled, "structured derivation still processing")
	}
	return &result, nil
}

func (s *Service) loadVerificationObservation(ctx context.Context, link core.ExperimentExecutionLink, predictions []core.PredictionBinding) (*QualifiedEvidenceSet, error) {
	if !predictionsNeedVerification(predictions) {
		return nil, nil
	}
	if s.verification == nil {
		return nil, core.NewReasonError(core.ReasonObservationNotSettled, "verification source unavailable")
	}
	cut, err := s.verification.AcquireVerificationObservationCut(ctx, operation.ID(link.OperationID))
	if err != nil {
		return nil, err
	}
	set, err := s.verification.QualifiedEvidenceForOperation(ctx, operation.ID(link.OperationID), cut)
	if err != nil {
		return nil, err
	}
	if set.Cut != cut {
		return nil, fmt.Errorf("verification observation cut changed during materialization")
	}
	return &set, nil
}

func evaluatePredictionResults(predictions []core.PredictionBinding, observation PredicateObservation) []core.PredictionResult {
	results := make([]core.PredictionResult, 0, len(predictions))
	for _, prediction := range predictions {
		evaluated := EvaluatePredicate(prediction.Predicate, observation)
		results = append(results, core.PredictionResult{PredictionID: prediction.PredictionID, Status: evaluated.Status, ReasonCode: evaluated.ReasonCode, BasisRefs: sortedRefs(evaluated.BasisRefs)})
	}
	return results
}

func predictionsNeedStructured(predictions []core.PredictionBinding) bool {
	for _, prediction := range predictions {
		if prediction.Predicate.Kind == core.PredicateStructuredTestStatus || prediction.Predicate.Kind == core.PredicateStructuredDiagnosticPresence {
			return true
		}
	}
	return false
}

func predictionsNeedVerification(predictions []core.PredictionBinding) bool {
	for _, prediction := range predictions {
		if prediction.Predicate.Kind == core.PredicateVerificationResult {
			return true
		}
	}
	return false
}

type observationCutDigestInput struct {
	SemanticsVersion int                         `json:"semantics_version"`
	Receipt          observationReceiptCut       `json:"receipt"`
	Structured       *observationStructuredCut   `json:"structured,omitempty"`
	Verification     *observationVerificationCut `json:"verification,omitempty"`
}

type observationReceiptCut struct {
	OperationID                   string `json:"operation_id"`
	SessionID                     string `json:"session_id"`
	RequestFingerprint            string `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string `json:"observation_binding_fingerprint,omitempty"`
	State                         string `json:"state"`
	Outcome                       string `json:"outcome"`
	OutputComplete                bool   `json:"output_complete"`
}

type observationStructuredCut struct {
	Status        structuredapp.InspectStatus `json:"status"`
	DerivationKey string                      `json:"derivation_key,omitempty"`
	Producer      *structuredcore.Producer    `json:"producer,omitempty"`
	ParseOutcome  structuredcore.ParseOutcome `json:"parse_outcome,omitempty"`
	Completeness  structuredcore.Completeness `json:"completeness,omitempty"`
	DetailsStatus structuredapp.DetailsStatus `json:"details_status"`
	RecordsExact  bool                        `json:"records_exact"`
	Truncated     bool                        `json:"truncated"`
}

type observationVerificationFact struct {
	EvidenceID       string                               `json:"evidence_id"`
	VerificationKind string                               `json:"verification_kind"`
	ProviderClass    verificationcore.ProviderClass       `json:"provider_class"`
	ProjectCommandID string                               `json:"project_command_id,omitempty"`
	Authority        verificationcore.DerivationAuthority `json:"authority"`
	Freshness        verificationcore.CandidateFreshness  `json:"freshness"`
	SourceGeneration string                               `json:"source_generation,omitempty"`
	Result           verificationcore.CandidateResult     `json:"result"`
}

type observationVerificationCut struct {
	IndexGeneration uint64                        `json:"index_generation"`
	Coverage        verificationcore.Coverage     `json:"coverage"`
	Facts           []observationVerificationFact `json:"facts,omitempty"`
}

func deriveObservationCutDigest(rec receipt.Receipt, structured *structuredapp.InspectResult, verification *QualifiedEvidenceSet) (string, error) {
	input := observationCutDigestInput{SemanticsVersion: observationSemanticsVersion, Receipt: observationReceiptCut{
		OperationID: rec.OperationID, SessionID: rec.SessionID, RequestFingerprint: rec.RequestFingerprint, ExecutionFingerprint: rec.ExecutionFingerprint,
		ObservationBindingFingerprint: rec.ObservationBindingFingerprint, State: string(rec.State), Outcome: string(rec.Outcome), OutputComplete: rec.OutputComplete,
	}}
	if structured != nil {
		input.Structured = &observationStructuredCut{Status: structured.Status, DerivationKey: structured.DerivationKey, Producer: structured.Producer, ParseOutcome: structured.ParseOutcome, Completeness: structured.Completeness, DetailsStatus: structured.Summary.DetailsStatus, RecordsExact: structured.Summary.RecordsTotalExact, Truncated: structured.Summary.Truncated}
	}
	if verification != nil {
		facts := make([]observationVerificationFact, 0, len(verification.Candidates))
		for _, candidate := range verification.Candidates {
			facts = append(facts, observationVerificationFact{EvidenceID: candidate.EvidenceID, VerificationKind: string(candidate.VerificationKind), ProviderClass: candidate.ProviderClass, ProjectCommandID: candidate.ProjectCommandID, Authority: candidate.Authority, Freshness: candidate.Freshness, SourceGeneration: candidate.SourceGeneration, Result: candidate.Result})
		}
		sort.Slice(facts, func(i, j int) bool { return facts[i].EvidenceID < facts[j].EvidenceID })
		input.Verification = &observationVerificationCut{IndexGeneration: verification.Cut.EvidenceIndexGeneration, Coverage: verification.Coverage, Facts: facts}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "cut_" + hex.EncodeToString(sum[:]), nil
}
