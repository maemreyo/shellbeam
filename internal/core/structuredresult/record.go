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

const RecordSchemaVersionV3 = 3

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
	SchemaVersion    int                `json:"schema_version"`
	RecordKind       RecordKind         `json:"record_kind"`
	Authority        Authority          `json:"authority"`
	DerivationMethod DerivationMethod   `json:"derivation_method"`
	Producer         Producer           `json:"producer"`
	OperationID      string             `json:"operation_id"`
	RecordID         string             `json:"record_id,omitempty"`
	SourceRef        StructuredInputRef `json:"source_ref"`
	Diagnostic       *Diagnostic        `json:"diagnostic,omitempty"`
	TestCase         *TestCase          `json:"test_case,omitempty"`
	TestSuite        *TestSuite         `json:"test_suite,omitempty"`
	ArtifactResult   *ArtifactResult    `json:"artifact_result,omitempty"`
}

type Diagnostic struct {
	Severity Severity              `json:"severity"`
	Code     string                `json:"code,omitempty"`
	Message  string                `json:"message"`
	Location source.SourceLocation `json:"location"`
}
type TestCase struct {
	Name                string                   `json:"name"`
	Package             string                   `json:"package,omitempty"`
	Status              TestStatus               `json:"status"`
	DurationMS          int64                    `json:"duration_ms,omitempty"`
	ProducerDisposition *ProducerTestDisposition `json:"producer_disposition,omitempty"`
	ProducerAddress     *ProducerTestAddress     `json:"producer_address,omitempty"`
	ArtifactEntry       *ArtifactTestEntryRef    `json:"artifact_entry,omitempty"`
	FailureExcerpt      *FailureExcerpt          `json:"failure_excerpt,omitempty"`
}
type TestSuite struct {
	Name       string              `json:"name"`
	Package    string              `json:"package,omitempty"`
	Status     TestStatus          `json:"status"`
	DurationMS int64               `json:"duration_ms,omitempty"`
	Aggregate  *TestSuiteAggregate `json:"aggregate,omitempty"`
}
type ArtifactResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersionV1 && r.SchemaVersion != SchemaVersion && r.SchemaVersion != RecordSchemaVersionV3 || r.Producer.Validate() != nil || r.SourceRef.Validate() != nil {
		return fmt.Errorf("invalid structured record metadata")
	}
	if r.SchemaVersion == SchemaVersionV1 && (r.SourceRef.Kind != StructuredInputRawOutput || r.RecordID != "" || recordHasV2Metadata(r) || recordHasV3Metadata(r)) {
		return fmt.Errorf("schema v1 record claims newer metadata")
	}
	if r.SchemaVersion == SchemaVersion && recordHasV3Metadata(r) {
		return fmt.Errorf("schema v2 record claims v3 metadata")
	}
	if r.SchemaVersion == RecordSchemaVersionV3 && !recordHasV3Metadata(r) {
		return fmt.Errorf("schema v3 record missing v3 metadata")
	}
	if r.RecordID != "" && !validDigest(r.RecordID) {
		return fmt.Errorf("invalid structured record id")
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
	if t.ProducerDisposition != nil && t.ProducerDisposition.Validate() != nil || t.ProducerAddress != nil && t.ProducerAddress.Validate() != nil || t.ArtifactEntry != nil && t.ArtifactEntry.Validate() != nil {
		return fmt.Errorf("invalid test case producer metadata")
	}
	if t.FailureExcerpt != nil {
		if t.Status != TestFailed && t.Status != TestSkipped || t.FailureExcerpt.Validate() != nil {
			return fmt.Errorf("invalid test case failure excerpt")
		}
	}
	return nil
}
func (t TestSuite) Validate() error {
	if !safeStructuredText(t.Name, 1024) || !validTestStatus(t.Status) || t.DurationMS < 0 || t.Aggregate != nil && t.Aggregate.Validate() != nil {
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

func recordHasV2Metadata(r Record) bool {
	return r.TestCase != nil && (r.TestCase.ProducerDisposition != nil || r.TestCase.ProducerAddress != nil || r.TestCase.ArtifactEntry != nil) || r.TestSuite != nil && r.TestSuite.Aggregate != nil
}

func recordHasV3Metadata(r Record) bool {
	return r.TestCase != nil && r.TestCase.FailureExcerpt != nil
}
