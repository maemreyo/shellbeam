package structuredresult

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/source"
)

type RecordKind string
type Authority string
type DerivationMethod string
type Severity string
type TestStatus string

const (
	RecordDiagnostic     RecordKind = "diagnostic"
	RecordTestCase       RecordKind = "test_case"
	RecordTestSuite      RecordKind = "test_suite"
	RecordArtifactResult RecordKind = "artifact_result"

	AuthorityMechanical Authority = "mechanical"
	AuthorityAdvisory   Authority = "advisory"

	DerivationNativeFieldMapping     DerivationMethod = "native_field_mapping"
	DerivationDeterministicNormalize DerivationMethod = "deterministic_normalization"
	DerivationHeuristicExtraction    DerivationMethod = "heuristic_extraction"

	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"

	TestPassed  TestStatus = "pass"
	TestFailed  TestStatus = "fail"
	TestSkipped TestStatus = "skip"
	TestError   TestStatus = "error"
)

type Record struct {
	SchemaVersion    int              `json:"schema_version"`
	RecordKind       RecordKind       `json:"record_kind"`
	Authority        Authority        `json:"authority"`
	DerivationMethod DerivationMethod `json:"derivation_method"`
	Producer         Producer         `json:"producer"`
	OperationID      string           `json:"operation_id"`
	SourceRef        RawOutputRef     `json:"source_ref"`
	Diagnostic       *Diagnostic      `json:"diagnostic,omitempty"`
	TestCase         *TestCase        `json:"test_case,omitempty"`
	TestSuite        *TestSuite       `json:"test_suite,omitempty"`
	ArtifactResult   *ArtifactResult  `json:"artifact_result,omitempty"`
}

type Diagnostic struct {
	Severity Severity              `json:"severity"`
	Code     string                `json:"code,omitempty"`
	Message  string                `json:"message"`
	Location source.SourceLocation `json:"location"`
}
type TestCase struct {
	Name       string     `json:"name"`
	Package    string     `json:"package,omitempty"`
	Status     TestStatus `json:"status"`
	DurationMS int64      `json:"duration_ms,omitempty"`
}
type TestSuite struct {
	Name       string     `json:"name"`
	Package    string     `json:"package,omitempty"`
	Status     TestStatus `json:"status"`
	DurationMS int64      `json:"duration_ms,omitempty"`
}
type ArtifactResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion || r.Producer.Validate() != nil || r.SourceRef.Validate() != nil {
		return fmt.Errorf("invalid structured record metadata")
	}
	if _, err := operation.ParseID(r.OperationID); err != nil {
		return err
	}
	if !validAuthority(r.Authority) || !validMethod(r.DerivationMethod) || (r.Authority == AuthorityMechanical && r.DerivationMethod == DerivationHeuristicExtraction) {
		return fmt.Errorf("invalid structured record authority")
	}
	branches := 0
	if r.Diagnostic != nil {
		branches++
		if r.RecordKind != RecordDiagnostic || r.Diagnostic.Validate() != nil {
			return fmt.Errorf("invalid diagnostic record")
		}
	}
	if r.TestCase != nil {
		branches++
		if r.RecordKind != RecordTestCase || r.TestCase.Validate() != nil {
			return fmt.Errorf("invalid test case record")
		}
	}
	if r.TestSuite != nil {
		branches++
		if r.RecordKind != RecordTestSuite || r.TestSuite.Validate() != nil {
			return fmt.Errorf("invalid test suite record")
		}
	}
	if r.ArtifactResult != nil {
		branches++
		if r.RecordKind != RecordArtifactResult || r.ArtifactResult.Validate() != nil {
			return fmt.Errorf("invalid artifact result")
		}
	}
	if branches != 1 {
		return fmt.Errorf("structured record requires one branch")
	}
	return nil
}

func (d Diagnostic) Validate() error {
	if !validSeverity(d.Severity) || !safeStructuredText(d.Message, 4096) || (d.Code != "" && !safeStructuredText(d.Code, 256)) {
		return fmt.Errorf("invalid diagnostic")
	}
	return d.Location.Validate()
}
func (t TestCase) Validate() error {
	if !safeStructuredText(t.Name, 1024) || !validTestStatus(t.Status) || t.DurationMS < 0 {
		return fmt.Errorf("invalid test case")
	}
	return nil
}
func (t TestSuite) Validate() error {
	if !safeStructuredText(t.Name, 1024) || !validTestStatus(t.Status) || t.DurationMS < 0 {
		return fmt.Errorf("invalid test suite")
	}
	return nil
}
func (a ArtifactResult) Validate() error {
	if !safeStructuredText(a.Name, 1024) || !safeStructuredText(a.Status, 128) {
		return fmt.Errorf("invalid artifact result")
	}
	return nil
}
func validAuthority(v Authority) bool { return v == AuthorityMechanical || v == AuthorityAdvisory }
func validMethod(v DerivationMethod) bool {
	return v == DerivationNativeFieldMapping || v == DerivationDeterministicNormalize || v == DerivationHeuristicExtraction
}
func validSeverity(v Severity) bool {
	return v == SeverityError || v == SeverityWarning || v == SeverityInfo
}
func validTestStatus(v TestStatus) bool {
	return v == TestPassed || v == TestFailed || v == TestSkipped || v == TestError
}
