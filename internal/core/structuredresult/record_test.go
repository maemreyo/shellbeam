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
		SchemaVersion: SchemaVersionV1, RecordKind: RecordDiagnostic, Authority: AuthorityMechanical,
		DerivationMethod: DerivationNativeFieldMapping, Producer: producer, OperationID: "op-1", SourceRef: RawInputRef(ref),
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

func TestFailureExcerptValidationIsClosedBoundedAndPathSafe(t *testing.T) {
	valid := FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "expected true\nreceived false", Truncated: false, Redacted: false}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid excerpt rejected: %v", err)
	}
	bad := []FailureExcerpt{
		{Namespace: "jest", VocabularyVersion: 2, Text: "bad vocabulary"},
		{Namespace: "jest", VocabularyVersion: 1, Text: strings.Repeat("x", MaxFailureExcerptBytes+1)},
		{Namespace: "jest", VocabularyVersion: 1, Text: "bad\x1b[31mansi"},
		{Namespace: "jest", VocabularyVersion: 1, Text: "bad\tcontrol"},
		{Namespace: "jest", VocabularyVersion: 1, Text: "at /tmp/private.ts:1:2"},
		{Namespace: "jest", VocabularyVersion: 1, Text: "   "},
	}
	for i, excerpt := range bad {
		if err := excerpt.Validate(); err == nil {
			t.Fatalf("bad excerpt %d accepted: %#v", i, excerpt)
		}
	}
}

func TestRecordSchemaV3OwnsFailureExcerptWithoutChangingIdentity(t *testing.T) {
	base := failureExcerptTestRecord(TestFailed)
	base.SchemaVersion = SchemaVersion
	base.RecordID = strings.Repeat("b", 64)
	if err := base.Validate(); err != nil {
		t.Fatalf("v2 base rejected: %v", err)
	}

	withExcerpt := base
	withExcerpt.SchemaVersion = RecordSchemaVersionV3
	withExcerpt.TestCase = cloneTestCaseForExcerpt(base.TestCase)
	withExcerpt.TestCase.FailureExcerpt = &FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure at src/a.ts:12"}
	if err := withExcerpt.Validate(); err != nil {
		t.Fatalf("v3 excerpt record rejected: %v", err)
	}
	if withExcerpt.RecordID != base.RecordID {
		t.Fatalf("excerpt changed record identity: got=%q want=%q", withExcerpt.RecordID, base.RecordID)
	}

	preV3 := withExcerpt
	preV3.SchemaVersion = SchemaVersion
	if err := preV3.Validate(); err == nil {
		t.Fatal("schema v2 record accepted failure excerpt")
	}
	missing := base
	missing.SchemaVersion = RecordSchemaVersionV3
	if err := missing.Validate(); err == nil {
		t.Fatal("schema v3 record without failure excerpt accepted")
	}
}

func TestFailureExcerptOnlyAttachesToFailOrSkipTestCases(t *testing.T) {
	for _, status := range []TestStatus{TestFailed, TestSkipped} {
		record := failureExcerptTestRecord(status)
		record.SchemaVersion = RecordSchemaVersionV3
		record.TestCase.FailureExcerpt = &FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "bounded failure"}
		if err := record.Validate(); err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
	}
	for _, status := range []TestStatus{TestPassed, TestError} {
		record := failureExcerptTestRecord(status)
		record.SchemaVersion = RecordSchemaVersionV3
		record.TestCase.FailureExcerpt = &FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "must not persist"}
		if err := record.Validate(); err == nil {
			t.Fatalf("status %q accepted failure excerpt", status)
		}
	}
}

func failureExcerptTestRecord(status TestStatus) Record {
	producer := Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	ref := RawOutputRef{SessionID: "session-excerpt", StartByte: 0, EndByte: 10, SHA256: strings.Repeat("a", 64)}
	return Record{
		SchemaVersion: SchemaVersion, RecordKind: RecordTestCase, Authority: AuthorityMechanical,
		DerivationMethod: DerivationNativeFieldMapping, Producer: producer, OperationID: "op-excerpt", SourceRef: RawInputRef(ref),
		TestCase: &TestCase{Name: "test excerpt", Status: status},
	}
}

func cloneTestCaseForExcerpt(in *TestCase) *TestCase {
	if in == nil {
		return nil
	}
	copy := *in
	return &copy
}
