package decisionprotocol

import (
	"fmt"
)

type PredictionRole string

const (
	PredictionRequired          PredictionRole = "REQUIRED_PREDICTION"
	PredictionDiscriminator     PredictionRole = "DISCRIMINATOR"
	PredictionObservationTarget PredictionRole = "OBSERVATION_TARGET"
)

func (r PredictionRole) Validate() error {
	switch r {
	case PredictionRequired, PredictionDiscriminator, PredictionObservationTarget:
		return nil
	}
	return fmt.Errorf("invalid prediction role %q", r)
}

type PredicateKind string

const (
	PredicateOperationOutcome             PredicateKind = "OPERATION_OUTCOME"
	PredicateStructuredTestStatus         PredicateKind = "STRUCTURED_TEST_STATUS"
	PredicateStructuredDiagnosticPresence PredicateKind = "STRUCTURED_DIAGNOSTIC_PRESENCE"
	PredicateVerificationResult           PredicateKind = "VERIFICATION_RESULT"
)

func (k PredicateKind) Validate() error {
	switch k {
	case PredicateOperationOutcome, PredicateStructuredTestStatus, PredicateStructuredDiagnosticPresence, PredicateVerificationResult:
		return nil
	}
	return fmt.Errorf("invalid predicate kind %q", k)
}

type OperationOutcome string

const (
	OperationSuccess OperationOutcome = "SUCCESS"
	OperationFailure OperationOutcome = "FAILURE"
	OperationTimeout OperationOutcome = "TIMEOUT"
	OperationKilled  OperationOutcome = "KILLED"
)

func (o OperationOutcome) Validate() error {
	switch o {
	case OperationSuccess, OperationFailure, OperationTimeout, OperationKilled:
		return nil
	}
	return fmt.Errorf("invalid operation outcome %q", o)
}

type OperationOutcomePredicate struct {
	ExpectedOutcome OperationOutcome `json:"expected_outcome"`
}

type StructuredTarget string

const (
	StructuredTargetTestCase  StructuredTarget = "TEST_CASE"
	StructuredTargetTestSuite StructuredTarget = "TEST_SUITE"
)

func (s StructuredTarget) Validate() error {
	if s == StructuredTargetTestCase || s == StructuredTargetTestSuite {
		return nil
	}
	return fmt.Errorf("invalid structured target %q", s)
}

type StructuredTestStatus string

const (
	StructuredTestPass  StructuredTestStatus = "PASS"
	StructuredTestFail  StructuredTestStatus = "FAIL"
	StructuredTestSkip  StructuredTestStatus = "SKIP"
	StructuredTestError StructuredTestStatus = "ERROR"
)

func (s StructuredTestStatus) Validate() error {
	switch s {
	case StructuredTestPass, StructuredTestFail, StructuredTestSkip, StructuredTestError:
		return nil
	}
	return fmt.Errorf("invalid structured test status %q", s)
}

type StructuredTestStatusPredicate struct {
	Target         StructuredTarget     `json:"target"`
	Package        string               `json:"package,omitempty"`
	Name           string               `json:"name"`
	ExpectedStatus StructuredTestStatus `json:"expected_status"`
}

type DiagnosticExpected string

const (
	DiagnosticPresent DiagnosticExpected = "PRESENT"
	DiagnosticAbsent  DiagnosticExpected = "ABSENT"
)

type StructuredDiagnosticPresencePredicate struct {
	Code     string             `json:"code"`
	Severity string             `json:"severity,omitempty"`
	Expected DiagnosticExpected `json:"expected"`
}

type VerificationExpectedResult string

const (
	VerificationPass       VerificationExpectedResult = "PASS"
	VerificationFail       VerificationExpectedResult = "FAIL"
	VerificationIncomplete VerificationExpectedResult = "INCOMPLETE"
	VerificationAmbiguous  VerificationExpectedResult = "AMBIGUOUS"
)

type VerificationResultPredicate struct {
	VerificationKind string                     `json:"verification_kind"`
	ProviderClass    string                     `json:"provider_class"`
	ProjectCommandID string                     `json:"project_command_id,omitempty"`
	ExpectedResult   VerificationExpectedResult `json:"expected_result"`
}

type ObservationPredicate struct {
	Kind                         PredicateKind                          `json:"kind"`
	OperationOutcome             *OperationOutcomePredicate             `json:"operation_outcome,omitempty"`
	StructuredTestStatus         *StructuredTestStatusPredicate         `json:"structured_test_status,omitempty"`
	StructuredDiagnosticPresence *StructuredDiagnosticPresencePredicate `json:"structured_diagnostic_presence,omitempty"`
	VerificationResult           *VerificationResultPredicate           `json:"verification_result,omitempty"`
}

func (p ObservationPredicate) Validate() error {
	if p.Kind.Validate() != nil {
		return fmt.Errorf("invalid observation predicate kind")
	}
	branches := 0
	if p.OperationOutcome != nil {
		branches++
	}
	if p.StructuredTestStatus != nil {
		branches++
	}
	if p.StructuredDiagnosticPresence != nil {
		branches++
	}
	if p.VerificationResult != nil {
		branches++
	}
	if branches != 1 {
		return fmt.Errorf("observation predicate requires exactly one branch")
	}
	switch p.Kind {
	case PredicateOperationOutcome:
		if p.OperationOutcome == nil || p.OperationOutcome.ExpectedOutcome.Validate() != nil {
			return fmt.Errorf("invalid operation outcome predicate")
		}
	case PredicateStructuredTestStatus:
		v := p.StructuredTestStatus
		if v == nil || v.Target.Validate() != nil || !boundedToken(v.Name, 512) || v.ExpectedStatus.Validate() != nil || (v.Package != "" && !boundedToken(v.Package, 512)) {
			return fmt.Errorf("invalid structured test predicate")
		}
	case PredicateStructuredDiagnosticPresence:
		v := p.StructuredDiagnosticPresence
		if v == nil || !boundedToken(v.Code, 256) || (v.Severity != "" && !boundedToken(v.Severity, 64)) || (v.Expected != DiagnosticPresent && v.Expected != DiagnosticAbsent) {
			return fmt.Errorf("invalid diagnostic predicate")
		}
	case PredicateVerificationResult:
		v := p.VerificationResult
		if v == nil || !boundedToken(v.VerificationKind, 128) || !boundedToken(v.ProviderClass, 128) || (v.ProjectCommandID != "" && !boundedToken(v.ProjectCommandID, 128)) {
			return fmt.Errorf("invalid verification predicate")
		}
		switch v.ExpectedResult {
		case VerificationPass, VerificationFail, VerificationIncomplete, VerificationAmbiguous:
		default:
			return fmt.Errorf("invalid verification result")
		}
	}
	return nil
}

func ObservationDimensionKey(p ObservationPredicate) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	type dimension struct {
		Version          int              `json:"version"`
		Kind             PredicateKind    `json:"kind"`
		Target           StructuredTarget `json:"target,omitempty"`
		Package          string           `json:"package,omitempty"`
		Name             string           `json:"name,omitempty"`
		Code             string           `json:"code,omitempty"`
		Severity         string           `json:"severity,omitempty"`
		VerificationKind string           `json:"verification_kind,omitempty"`
		ProviderClass    string           `json:"provider_class,omitempty"`
		ProjectCommandID string           `json:"project_command_id,omitempty"`
	}
	d := dimension{Version: 1, Kind: p.Kind}
	switch p.Kind {
	case PredicateStructuredTestStatus:
		d.Target = p.StructuredTestStatus.Target
		d.Package = p.StructuredTestStatus.Package
		d.Name = p.StructuredTestStatus.Name
	case PredicateStructuredDiagnosticPresence:
		d.Code = p.StructuredDiagnosticPresence.Code
		d.Severity = p.StructuredDiagnosticPresence.Severity
	case PredicateVerificationResult:
		d.VerificationKind = p.VerificationResult.VerificationKind
		d.ProviderClass = p.VerificationResult.ProviderClass
		d.ProjectCommandID = p.VerificationResult.ProjectCommandID
	}
	return canonicalHash("dim_", d)
}

type PredictionEvaluationStatus string

const (
	PredictionNotEvaluated  PredictionEvaluationStatus = "NOT_EVALUATED"
	PredictionMatch         PredictionEvaluationStatus = "MATCH"
	PredictionMismatch      PredictionEvaluationStatus = "MISMATCH"
	PredictionIndeterminate PredictionEvaluationStatus = "INDETERMINATE"
)

func (s PredictionEvaluationStatus) Validate() error {
	switch s {
	case PredictionNotEvaluated, PredictionMatch, PredictionMismatch, PredictionIndeterminate:
		return nil
	}
	return fmt.Errorf("invalid prediction evaluation status %q", s)
}
