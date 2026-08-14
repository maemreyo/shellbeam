package structuredresult

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/source"
)

func TestRecordAuthorityKindsAndRawOutputProvenance(t *testing.T) {
	producer := Producer{AdapterID: "go-vet-json", AdapterVersion: 1, CapabilityVersion: 1}
	ref := RawOutputRef{SessionID: "session-1", StartByte: 10, EndByte: 90, SHA256: strings.Repeat("a", 64)}
	record := Record{
		SchemaVersion: 1, RecordKind: RecordDiagnostic, Authority: AuthorityMechanical,
		DerivationMethod: DerivationNativeFieldMapping, Producer: producer, OperationID: "op-1", SourceRef: ref,
		Diagnostic: &Diagnostic{Severity: SeverityError, Code: "printf", Message: "bad printf", Location: source.SourceLocation{Kind: source.LocationProviderReported, ProviderReported: &source.ProviderReportedLocation{Origin: source.OriginRepository, SanitizedLogicalPath: "main.go", Line: 5, Column: 2, NormalizationQuality: source.NormalizationPartial}}},
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := record
	bad.Authority = AuthorityMechanical
	bad.DerivationMethod = DerivationHeuristicExtraction
	if err := bad.Validate(); err == nil {
		t.Fatal("heuristic record received mechanical authority")
	}
	bad = record
	bad.TestCase = &TestCase{Name: "TestX", Status: TestPassed}
	if err := bad.Validate(); err == nil {
		t.Fatal("multiple record branches accepted")
	}
}

func TestStructuredRecordRejectsControlBearingSemanticText(t *testing.T) {
	if err := (TestCase{Name: "bad\nname", Status: TestPassed}).Validate(); err == nil {
		t.Fatal("control-bearing test name accepted")
	}
	if err := (ArtifactResult{Name: "artifact", Status: "bad\rstatus"}).Validate(); err == nil {
		t.Fatal("control-bearing artifact status accepted")
	}
}
