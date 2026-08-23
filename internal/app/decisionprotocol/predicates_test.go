package decisionprotocol

import (
	"strings"
	"testing"
	"time"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	evidencecore "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

const predicateGeneration = "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestOperationOutcomePredicateMatrix(t *testing.T) {
	predicate := core.ObservationPredicate{Kind: core.PredicateOperationOutcome, OperationOutcome: &core.OperationOutcomePredicate{ExpectedOutcome: core.OperationSuccess}}
	cases := []struct {
		name    string
		receipt receipt.Receipt
		want    core.PredictionEvaluationStatus
	}{
		{"success", receipt.Receipt{State: session.Completed, Outcome: session.Success}, core.PredictionMatch},
		{"failure", receipt.Receipt{State: session.Failed, Outcome: session.Failure}, core.PredictionMismatch},
		{"timeout", receipt.Receipt{State: session.TimedOut, Outcome: session.Timeout}, core.PredictionMismatch},
		{"killed", receipt.Receipt{State: session.Killed, Outcome: session.KilledOutcome}, core.PredictionMismatch},
		{"nonterminal", receipt.Receipt{State: session.Running, Outcome: session.NoOutcome}, core.PredictionNotEvaluated},
		{"unknown_terminal", receipt.Receipt{State: session.Abandoned, Outcome: session.Ambiguous}, core.PredictionIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluatePredicate(predicate, PredicateObservation{Receipt: &tc.receipt})
			if got.Status != tc.want {
				t.Fatalf("got=%#v want=%s", got, tc.want)
			}
		})
	}
}

func TestStructuredTestStatusPredicateExactIdentityAndAmbiguity(t *testing.T) {
	predicate := core.ObservationPredicate{Kind: core.PredicateStructuredTestStatus, StructuredTestStatus: &core.StructuredTestStatusPredicate{Target: core.StructuredTargetTestCase, Package: "example/a", Name: "TestRace", ExpectedStatus: core.StructuredTestPass}}
	pass := structuredTestRecord("TestRace", "example/a", structuredcore.TestPassed)
	fail := structuredTestRecord("TestRace", "example/a", structuredcore.TestFailed)
	cases := []struct {
		name   string
		result structuredapp.InspectResult
		want   core.PredictionEvaluationStatus
	}{
		{"match", terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete, pass), core.PredictionMatch},
		{"mismatch", terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete, fail), core.PredictionMismatch},
		{"absent", terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete, structuredTestRecord("TestOther", "example/a", structuredcore.TestPassed)), core.PredictionNotEvaluated},
		{"ambiguous", terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete, pass, fail), core.PredictionIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluatePredicate(predicate, PredicateObservation{Structured: &tc.result})
			if got.Status != tc.want {
				t.Fatalf("got=%#v want=%s", got, tc.want)
			}
		})
	}
}

func TestStructuredDiagnosticPresenceRequiresCompleteNegativeObservation(t *testing.T) {
	predicate := core.ObservationPredicate{Kind: core.PredicateStructuredDiagnosticPresence, StructuredDiagnosticPresence: &core.StructuredDiagnosticPresencePredicate{Code: "printf", Severity: "error", Expected: core.DiagnosticAbsent}}
	matching := structuredDiagnosticRecord("printf", structuredcore.SeverityError)
	completeZero := terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete)
	partialZero := terminalStructured(structuredcore.ParsePartial, structuredcore.CompletenessPartial)
	unavailableZero := terminalStructured(structuredcore.ParseUnavailable, structuredcore.CompletenessUnavailable)
	if got := EvaluatePredicate(predicate, PredicateObservation{Structured: &matching}); got.Status != core.PredictionMismatch {
		t.Fatalf("present=%#v", got)
	}
	if got := EvaluatePredicate(predicate, PredicateObservation{Structured: &completeZero}); got.Status != core.PredictionMatch {
		t.Fatalf("complete zero=%#v", got)
	}
	if got := EvaluatePredicate(predicate, PredicateObservation{Structured: &partialZero}); got.Status != core.PredictionIndeterminate {
		t.Fatalf("partial zero=%#v", got)
	}
	if got := EvaluatePredicate(predicate, PredicateObservation{Structured: &unavailableZero}); got.Status != core.PredictionIndeterminate {
		t.Fatalf("unavailable zero=%#v", got)
	}
}

func TestVerificationResultPredicateUsesCompleteQualifiedOperationSet(t *testing.T) {
	predicate := core.ObservationPredicate{Kind: core.PredicateVerificationResult, VerificationResult: &core.VerificationResultPredicate{VerificationKind: "test", ProviderClass: string(verificationcore.ProviderIntegrationTest), ExpectedResult: core.VerificationPass}}
	pass := verificationCandidate('a', verificationcore.CandidatePass)
	fail := verificationCandidate('b', verificationcore.CandidateFail)
	advisory := verificationCandidate('c', verificationcore.CandidatePass)
	advisory.Authority = verificationcore.AuthorityAdvisory
	stale := verificationCandidate('d', verificationcore.CandidatePass)
	stale.Freshness = verificationcore.CandidateStale
	wrongOp := verificationCandidate('e', verificationcore.CandidatePass)
	wrongOp.OperationID = "op-other"
	cases := []struct {
		name string
		set  QualifiedEvidenceSet
		want core.PredictionEvaluationStatus
	}{
		{"pass", qualifiedSet(verificationcore.CoverageComplete, pass), core.PredictionMatch},
		{"fail", qualifiedSet(verificationcore.CoverageComplete, fail), core.PredictionMismatch},
		{"conflict", qualifiedSet(verificationcore.CoverageComplete, pass, fail), core.PredictionIndeterminate},
		{"advisory", qualifiedSet(verificationcore.CoverageComplete, advisory), core.PredictionIndeterminate},
		{"stale", qualifiedSet(verificationcore.CoverageComplete, stale), core.PredictionIndeterminate},
		{"bounded", qualifiedSet(verificationcore.CoverageBounded, pass), core.PredictionIndeterminate},
		{"unrelated_only", qualifiedSet(verificationcore.CoverageComplete, wrongOp), core.PredictionNotEvaluated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluatePredicate(predicate, PredicateObservation{OperationID: "op-predicate", SourceGeneration: predicateGeneration, Verification: &tc.set})
			if got.Status != tc.want {
				t.Fatalf("got=%#v want=%s", got, tc.want)
			}
		})
	}
}

func terminalStructured(outcome structuredcore.ParseOutcome, completeness structuredcore.Completeness, records ...structuredcore.Record) structuredapp.InspectResult {
	return structuredapp.InspectResult{SchemaVersion: 1, OperationID: "op-predicate", Status: structuredapp.InspectTerminal, DerivationKey: strings.Repeat("1", 64), ParseOutcome: outcome, Completeness: completeness, Summary: structuredapp.InspectSummary{DetailsStatus: structuredapp.DetailsAvailable, RecordsTotalExact: true, RecordsReturned: len(records), RecordsTotalOrLowerBound: len(records)}, Records: records}
}

func structuredTestRecord(name, pkg string, status structuredcore.TestStatus) structuredcore.Record {
	return structuredcore.Record{RecordKind: structuredcore.RecordTestCase, TestCase: &structuredcore.TestCase{Name: name, Package: pkg, Status: status}}
}

func structuredDiagnosticRecord(code string, severity structuredcore.Severity) structuredapp.InspectResult {
	record := structuredcore.Record{RecordKind: structuredcore.RecordDiagnostic, Diagnostic: &structuredcore.Diagnostic{Code: code, Severity: severity}}
	return terminalStructured(structuredcore.ParseComplete, structuredcore.CompletenessComplete, record)
}

func verificationCandidate(suffix byte, result verificationcore.CandidateResult) verificationcore.EvidenceCandidate {
	return verificationcore.EvidenceCandidate{
		EvidenceID: "ev_" + strings.Repeat(string(suffix), 64), VerificationKind: evidencecore.VerificationTest,
		ProviderClass: verificationcore.ProviderIntegrationTest, ProviderClassKnown: true,
		OperationID: "op-predicate", SessionID: "session-1", WorkspaceID: "ws_01K00000000000000000000000",
		SourceGeneration: predicateGeneration, ContractDigest: strings.Repeat("b", 64), SemanticContractDigest: strings.Repeat("c", 64),
		Authority: verificationcore.AuthorityMechanical, AuthorityKnown: true, Freshness: verificationcore.CandidateCurrent, Result: result, CompletedAt: time.Unix(10, 0).UTC(),
	}
}

func qualifiedSet(coverage verificationcore.Coverage, candidates ...verificationcore.EvidenceCandidate) QualifiedEvidenceSet {
	return QualifiedEvidenceSet{Cut: VerificationObservationCut{EvidenceIndexGeneration: 7}, Candidates: candidates, Coverage: coverage}
}
