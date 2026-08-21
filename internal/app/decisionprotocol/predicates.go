package decisionprotocol

import (
	"sort"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

type VerificationObservationCut struct {
	EvidenceIndexGeneration uint64 `json:"evidence_index_generation"`
}

type QualifiedEvidenceSet struct {
	Cut        VerificationObservationCut           `json:"cut"`
	Candidates []verificationcore.EvidenceCandidate `json:"candidates,omitempty"`
	Coverage   verificationcore.Coverage            `json:"coverage"`
}

type PredicateObservation struct {
	OperationID      string
	SourceGeneration string
	Receipt          *receipt.Receipt
	Structured       *structuredapp.InspectResult
	Verification     *QualifiedEvidenceSet
}

type PredicateEvaluation struct {
	Status     core.PredictionEvaluationStatus
	ReasonCode string
	BasisRefs  []string
}

func EvaluatePredicate(predicate core.ObservationPredicate, observation PredicateObservation) PredicateEvaluation {
	if err := predicate.Validate(); err != nil {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "invalid_predicate"}
	}
	switch predicate.Kind {
	case core.PredicateOperationOutcome:
		return evaluateOperationOutcome(*predicate.OperationOutcome, observation.Receipt)
	case core.PredicateStructuredTestStatus:
		return evaluateStructuredTest(*predicate.StructuredTestStatus, observation.Structured)
	case core.PredicateStructuredDiagnosticPresence:
		return evaluateStructuredDiagnostic(*predicate.StructuredDiagnosticPresence, observation.Structured)
	case core.PredicateVerificationResult:
		return evaluateVerificationResult(*predicate.VerificationResult, observation)
	default:
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "unsupported_predicate"}
	}
}

func evaluateOperationOutcome(predicate core.OperationOutcomePredicate, rec *receipt.Receipt) PredicateEvaluation {
	if rec == nil || !rec.State.Terminal() {
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "operation_not_terminal"}
	}
	actual := core.OperationOutcome("")
	switch rec.Outcome {
	case "success":
		actual = core.OperationSuccess
	case "failure":
		actual = core.OperationFailure
	case "timeout":
		actual = core.OperationTimeout
	case "killed":
		actual = core.OperationKilled
	default:
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "operation_outcome_unknown"}
	}
	if actual == predicate.ExpectedOutcome {
		return PredicateEvaluation{Status: core.PredictionMatch, BasisRefs: receiptBasis(rec)}
	}
	return PredicateEvaluation{Status: core.PredictionMismatch, BasisRefs: receiptBasis(rec)}
}

func evaluateStructuredTest(predicate core.StructuredTestStatusPredicate, result *structuredapp.InspectResult) PredicateEvaluation {
	if result == nil || result.Status != structuredapp.InspectTerminal {
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "structured_not_terminal"}
	}
	if !structuredDetailsUsable(result) {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "structured_details_incomplete"}
	}
	matches := make([]structuredcore.Record, 0, 2)
	for _, record := range result.Records {
		if structuredTestMatches(record, predicate) {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "structured_test_absent"}
	}
	if len(matches) != 1 || matches[0].Authority == structuredcore.AuthorityAdvisory {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "ambiguous_test_cardinality"}
	}
	actual := structuredStatus(matches[0])
	if actual == predicate.ExpectedStatus {
		return PredicateEvaluation{Status: core.PredictionMatch, BasisRefs: []string{structuredBasis(result, matches[0])}}
	}
	return PredicateEvaluation{Status: core.PredictionMismatch, BasisRefs: []string{structuredBasis(result, matches[0])}}
}

func evaluateStructuredDiagnostic(predicate core.StructuredDiagnosticPresencePredicate, result *structuredapp.InspectResult) PredicateEvaluation {
	if result == nil || result.Status != structuredapp.InspectTerminal {
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "structured_not_terminal"}
	}
	found := false
	advisory := false
	for _, record := range result.Records {
		if !structuredDiagnosticMatches(record, predicate) {
			continue
		}
		found = true
		advisory = advisory || record.Authority == structuredcore.AuthorityAdvisory
	}
	if found {
		if advisory {
			return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "diagnostic_authority_advisory"}
		}
		if predicate.Expected == core.DiagnosticPresent {
			return PredicateEvaluation{Status: core.PredictionMatch, BasisRefs: []string{"structured:" + result.DerivationKey}}
		}
		return PredicateEvaluation{Status: core.PredictionMismatch, BasisRefs: []string{"structured:" + result.DerivationKey}}
	}
	if !structuredCompleteNegative(result) {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "structured_negative_incomplete"}
	}
	if predicate.Expected == core.DiagnosticAbsent {
		return PredicateEvaluation{Status: core.PredictionMatch, BasisRefs: []string{"structured:" + result.DerivationKey}}
	}
	return PredicateEvaluation{Status: core.PredictionMismatch, BasisRefs: []string{"structured:" + result.DerivationKey}}
}

func evaluateVerificationResult(predicate core.VerificationResultPredicate, observation PredicateObservation) PredicateEvaluation {
	set := observation.Verification
	if set == nil {
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "verification_not_observed"}
	}
	if set.Coverage != verificationcore.CoverageComplete {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "verification_coverage_incomplete"}
	}
	selectorSeen := false
	unqualifiedSeen := false
	qualified := make([]verificationcore.EvidenceCandidate, 0, len(set.Candidates))
	for _, candidate := range set.Candidates {
		if candidate.OperationID != observation.OperationID || string(candidate.VerificationKind) != predicate.VerificationKind || string(candidate.ProviderClass) != predicate.ProviderClass || candidate.ProjectCommandID != predicate.ProjectCommandID {
			continue
		}
		selectorSeen = true
		if candidate.Validate() != nil || !candidate.ProviderClassKnown || !candidate.AuthorityKnown || !verificationcore.MeetsMinimumAuthority(candidate.Authority, verificationcore.AuthorityMechanical) || candidate.Freshness != verificationcore.CandidateCurrent || candidate.SourceGeneration != observation.SourceGeneration {
			unqualifiedSeen = true
			continue
		}
		qualified = append(qualified, candidate)
	}
	if len(qualified) == 0 {
		if unqualifiedSeen {
			return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "verification_unqualified"}
		}
		if selectorSeen {
			return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "verification_unresolved"}
		}
		return PredicateEvaluation{Status: core.PredictionNotEvaluated, ReasonCode: "verification_absent"}
	}
	result := qualified[0].Result
	refs := make([]string, 0, len(qualified))
	for _, candidate := range qualified {
		refs = append(refs, candidate.EvidenceID)
		if candidate.Result != result {
			return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "conflicting_verification_results", BasisRefs: sortedRefs(refs)}
		}
	}
	if unqualifiedSeen {
		return PredicateEvaluation{Status: core.PredictionIndeterminate, ReasonCode: "verification_unqualified", BasisRefs: sortedRefs(refs)}
	}
	actual := verificationResult(result)
	if actual == predicate.ExpectedResult {
		return PredicateEvaluation{Status: core.PredictionMatch, BasisRefs: sortedRefs(refs)}
	}
	return PredicateEvaluation{Status: core.PredictionMismatch, BasisRefs: sortedRefs(refs)}
}

func structuredDetailsUsable(result *structuredapp.InspectResult) bool {
	return result.Status == structuredapp.InspectTerminal && result.Summary.DetailsStatus == structuredapp.DetailsAvailable && result.Summary.RecordsTotalExact && !result.Summary.Truncated
}

func structuredCompleteNegative(result *structuredapp.InspectResult) bool {
	return structuredDetailsUsable(result) && result.ParseOutcome == structuredcore.ParseComplete && result.Completeness == structuredcore.CompletenessComplete
}

func structuredTestMatches(record structuredcore.Record, predicate core.StructuredTestStatusPredicate) bool {
	if predicate.Target == core.StructuredTargetTestCase {
		return record.RecordKind == structuredcore.RecordTestCase && record.TestCase != nil && record.TestCase.Name == predicate.Name && record.TestCase.Package == predicate.Package
	}
	return record.RecordKind == structuredcore.RecordTestSuite && record.TestSuite != nil && record.TestSuite.Name == predicate.Name && record.TestSuite.Package == predicate.Package
}

func structuredStatus(record structuredcore.Record) core.StructuredTestStatus {
	status := structuredcore.TestStatus("")
	if record.TestCase != nil {
		status = record.TestCase.Status
	} else if record.TestSuite != nil {
		status = record.TestSuite.Status
	}
	switch status {
	case structuredcore.TestPassed:
		return core.StructuredTestPass
	case structuredcore.TestFailed:
		return core.StructuredTestFail
	case structuredcore.TestSkipped:
		return core.StructuredTestSkip
	case structuredcore.TestError:
		return core.StructuredTestError
	default:
		return ""
	}
}

func structuredDiagnosticMatches(record structuredcore.Record, predicate core.StructuredDiagnosticPresencePredicate) bool {
	return record.RecordKind == structuredcore.RecordDiagnostic && record.Diagnostic != nil && record.Diagnostic.Code == predicate.Code && (predicate.Severity == "" || string(record.Diagnostic.Severity) == predicate.Severity)
}

func verificationResult(result verificationcore.CandidateResult) core.VerificationExpectedResult {
	switch result {
	case verificationcore.CandidatePass:
		return core.VerificationPass
	case verificationcore.CandidateFail:
		return core.VerificationFail
	case verificationcore.CandidateIncomplete:
		return core.VerificationIncomplete
	case verificationcore.CandidateAmbiguous:
		return core.VerificationAmbiguous
	default:
		return ""
	}
}

func receiptBasis(rec *receipt.Receipt) []string {
	if rec == nil || rec.OperationID == "" {
		return nil
	}
	return []string{"receipt:" + rec.OperationID}
}

func structuredBasis(result *structuredapp.InspectResult, _ structuredcore.Record) string {
	return "structured:" + result.DerivationKey
}

func sortedRefs(refs []string) []string {
	out := append([]string(nil), refs...)
	sort.Strings(out)
	return out
}
