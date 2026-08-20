package structuredresult

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type inspectRepoFake struct {
	found        bool
	derivation   core.Derivation
	summary      RecordSummary
	summaryFound bool
	records      []core.Record
}

func (r *inspectRepoFake) FindOperationDerivation(context.Context, string) (core.Derivation, bool, error) {
	return r.derivation, r.found, nil
}
func (r *inspectRepoFake) GetRecordSummary(context.Context, string) (RecordSummary, bool, error) {
	return r.summary, r.summaryFound, nil
}
func (r *inspectRepoFake) ListRecords(_ context.Context, _ string, q RecordQuery) ([]core.Record, error) {
	if q.Offset > len(r.records) {
		return nil, errInvalidRecordQuery
	}
	end := min(q.Offset+q.Limit, len(r.records))
	return append([]core.Record(nil), r.records[q.Offset:end]...), nil
}

func TestStructuredInspectDistinguishesLifecycleAndTerminalOutcome(t *testing.T) {
	cases := []struct {
		name         string
		found        bool
		lifecycle    core.Lifecycle
		outcome      core.ParseOutcome
		completeness core.Completeness
		status       InspectStatus
	}{
		{"not found", false, "", "", "", InspectNotFound},
		{"pending", true, core.LifecyclePending, "", core.CompletenessUnavailable, InspectPending},
		{"processing", true, core.LifecycleProcessing, "", core.CompletenessUnavailable, InspectProcessing},
		{"complete", true, core.LifecycleTerminal, core.ParseComplete, core.CompletenessComplete, InspectTerminal},
		{"partial", true, core.LifecycleTerminal, core.ParsePartial, core.CompletenessPartial, InspectTerminal},
		{"malformed", true, core.LifecycleTerminal, core.ParseMalformed, core.CompletenessPartial, InspectTerminal},
		{"unavailable", true, core.LifecycleTerminal, core.ParseUnavailable, core.CompletenessUnavailable, InspectTerminal},
		{"budget", true, core.LifecycleTerminal, core.ParseBudgetExceeded, core.CompletenessPartial, InspectTerminal},
		{"compacted", true, core.LifecycleTerminal, core.ParseComplete, core.CompletenessCompacted, InspectTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &inspectRepoFake{found: tc.found, derivation: inspectDerivation(tc.lifecycle, tc.outcome, tc.completeness)}
			if tc.completeness == core.CompletenessCompacted {
				repo.summary = RecordSummary{RecordsTotal: 3, Errors: 1, TestPassed: 2, Compacted: true}
				repo.summaryFound = true
			}
			inspector := newInspectService(t, repo)
			got, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "op-1", MaxRecords: 10})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status {
				t.Fatalf("status=%s", got.Status)
			}
			if tc.status == InspectTerminal && (got.ParseOutcome != tc.outcome || got.Completeness != tc.completeness) {
				t.Fatalf("result=%#v", got)
			}
			if tc.completeness == core.CompletenessCompacted && (got.Summary.DetailsStatus != DetailsCompacted || got.Summary.RecordsTotalOrLowerBound != 3 || len(got.Records) != 0) {
				t.Fatalf("compacted=%#v", got)
			}
		})
	}
}

func TestStructuredInspectFiltersPaginatesAndPreservesGlobalSummary(t *testing.T) {
	d := inspectDerivation(core.LifecycleTerminal, core.ParseComplete, core.CompletenessComplete)
	repo := &inspectRepoFake{found: true, derivation: d, summaryFound: true, summary: RecordSummary{RecordsTotal: 5, Errors: 2, Warnings: 1, Files: 2, TestPassed: 1, TestFailed: 1, Mechanical: 4, Advisory: 1}}
	repo.records = []core.Record{
		diagnosticRecord(d, "error", "internal/a.go", "E1"), diagnosticRecord(d, "warning", "internal/b.go", "W1"),
		testRecord(d, "TestPass", core.TestPassed), diagnosticRecord(d, "error", "internal/a.go", "E2"), testRecord(d, "TestFail", core.TestFailed),
	}
	inspector := newInspectService(t, repo)
	filter := RecordFilter{RecordKind: "diagnostic", Severity: "error", Path: "internal/a.go"}
	first, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "op-1", Filter: filter, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 1 || first.Records[0].Diagnostic.Code != "E1" || first.Continuation == "" || !first.Summary.Truncated || first.Summary.RecordsReturned != 1 || first.Summary.Errors != 2 || first.Summary.TestPassed != 1 {
		t.Fatalf("first=%#v", first)
	}
	second, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "op-1", Filter: filter, Continuation: first.Continuation, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].Diagnostic.Code != "E2" || second.Continuation != "" || second.Summary.Truncated || !second.Summary.RecordsTotalExact || second.Summary.RecordsTotalOrLowerBound != 2 {
		t.Fatalf("second=%#v", second)
	}
	if _, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "op-1", Filter: RecordFilter{Severity: "bogus"}, MaxRecords: 1}); err == nil {
		t.Fatal("invalid severity accepted")
	}
}

func newInspectService(t *testing.T, repo InspectionRepository) *Inspector {
	t.Helper()
	codec, err := NewResultCursorCodec(structuredCursorKey("0"))
	if err != nil {
		t.Fatal(err)
	}
	return NewInspector(repo, codec)
}
func inspectDerivation(l core.Lifecycle, o core.ParseOutcome, c core.Completeness) core.Derivation {
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 1, SHA256: strings.Repeat("a", 64)}
	producer := core.Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	key, _ := core.DerivationKey([]core.RawOutputRef{ref}, producer, 1, strings.Repeat("b", 64))
	return core.Derivation{SchemaVersion: core.SchemaVersionV1, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{core.RawInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("b", 64), Lifecycle: l, ParseOutcome: o, Completeness: c}
}
func diagnosticRecord(d core.Derivation, severity, path, code string) core.Record {
	return core.Record{SchemaVersion: core.SchemaVersionV1, RecordKind: core.RecordDiagnostic, Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping, Producer: d.Producer, OperationID: "op-1", SourceRef: d.SourceAuthorityRefs[0], Diagnostic: &core.Diagnostic{Severity: core.Severity(severity), Code: code, Message: "message", Location: source.SourceLocation{Kind: source.LocationProviderReported, ProviderReported: &source.ProviderReportedLocation{Origin: source.OriginRepository, SanitizedLogicalPath: path, Line: 1, Column: 1, NormalizationQuality: source.NormalizationPartial}}}}
}
func testRecord(d core.Derivation, name string, status core.TestStatus) core.Record {
	return core.Record{SchemaVersion: core.SchemaVersionV1, RecordKind: core.RecordTestCase, Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping, Producer: d.Producer, OperationID: "op-1", SourceRef: d.SourceAuthorityRefs[0], TestCase: &core.TestCase{Name: name, Package: "example", Status: status}}
}

type artifactInspectRepository struct {
	*inspectRepoFake
	state InputSourceState
}

func (r *artifactInspectRepository) ResolveArtifactInputState(context.Context, core.ArtifactBlobRef) (InputSourceState, error) {
	return r.state, nil
}

func TestInspectArtifactReportsSourceStateAndSemanticsCoverageWithoutPrivatePath(t *testing.T) {
	ref := core.ArtifactBlobRef{SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: "abl_" + strings.Repeat("a", 64), OperationID: "inspect-artifact-op", SessionID: "inspect-artifact-session", RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q", DeclaredPath: "reports/junit.xml", NormalizedWorkspacePath: "reports/junit.xml", SHA256: strings.Repeat("b", 64), Size: 12, TerminalCut: core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 2, ReceiptDigest: strings.Repeat("c", 64)}, ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("d", 64)}}
	producer := core.Producer{AdapterID: "pytest-junit-xml", AdapterVersion: 1, CapabilityVersion: 1}
	key, err := core.DerivationKeyForInputs([]core.StructuredInputRef{core.ArtifactInputRef(ref)}, producer, 1, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	coverage := &core.ProducerSemanticsCoverage{Namespace: "pytest", VocabularyVersion: 1, Format: "junit-xml", Family: "xunit2", MechanicallyObservable: []string{"coarse:pass"}, Unavailable: []string{"pytest:xpass_exact"}}
	d := core.Derivation{SchemaVersion: core.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{core.ArtifactInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("e", 64), Lifecycle: core.LifecycleTerminal, ParseOutcome: core.ParseComplete, Completeness: core.CompletenessComplete, SemanticsCoverage: coverage}
	base := &inspectRepoFake{derivation: d, found: true, summary: RecordSummary{}, summaryFound: true}
	inspector := newInspectService(t, &artifactInspectRepository{inspectRepoFake: base, state: InputSourceRetained})
	got, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "inspect-artifact-op", MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceKind != core.StructuredInputArtifactBlob || got.SourceState != InputSourceRetained || got.SemanticsCoverage == nil || got.SemanticsCoverage.Family != "xunit2" {
		t.Fatalf("inspect=%#v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "/private/") || strings.Contains(string(encoded), "content") {
		t.Fatalf("private blob data leaked: %s", encoded)
	}
	got.SemanticsCoverage.Unavailable[0] = "changed"
	if coverage.Unavailable[0] != "pytest:xpass_exact" {
		t.Fatal("inspect coverage aliases derivation")
	}
}

func TestInspectReportsTerminalReasonAndObservedEntriesDefensively(t *testing.T) {
	ref := core.RawOutputRef{SessionID: "inspect-v3-session", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("7", 64)}
	producer := core.Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	key, err := core.DerivationKeyForInputs([]core.StructuredInputRef{core.RawInputRef(ref)}, producer, 1, strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	counts := &core.ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 2, Pass: 1, Fail: 1}
	d := core.Derivation{SchemaVersion: core.DerivationSchemaVersionV3, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{core.RawInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("8", 64), Lifecycle: core.LifecycleTerminal, ParseOutcome: core.ParsePartial, Completeness: core.CompletenessPartial, CompletenessReason: core.CompletenessReasonPassRecordsElided, ObservedEntries: counts}
	inspector := newInspectService(t, &inspectRepoFake{derivation: d, found: true, summary: RecordSummary{}, summaryFound: true})
	got, err := inspector.Inspect(context.Background(), InspectRequest{OperationID: "inspect-v3-op", MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletenessReason != core.CompletenessReasonPassRecordsElided || got.ObservedEntries == nil || *got.ObservedEntries != *counts {
		t.Fatalf("inspect metadata=%#v", got)
	}
	got.ObservedEntries.Fail = 0
	if counts.Fail != 1 {
		t.Fatal("inspect observed counts alias derivation")
	}
}
