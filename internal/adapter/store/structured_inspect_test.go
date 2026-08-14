package store

import (
	"context"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestStructuredOperationIndexIsIdempotentConflictClosedAndReopens(t *testing.T) {
	root := t.TempDir() + "/state"
	r := openStructuredRepositoryAt(t, root)
	d := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	if err := r.PutDerivation(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := r.BindOperationDerivation(context.Background(), operation.ID("op-inspect"), d.DerivationKey); err != nil {
		t.Fatal(err)
	}
	if err := r.BindOperationDerivation(context.Background(), operation.ID("op-inspect"), d.DerivationKey); err != nil {
		t.Fatal(err)
	}
	other := strings.Repeat("b", 64)
	if err := r.BindOperationDerivation(context.Background(), operation.ID("op-inspect"), other); err == nil {
		t.Fatal("conflicting index accepted")
	}
	r = openStructuredRepositoryAt(t, root)
	got, found, err := r.FindOperationDerivation(context.Background(), "op-inspect")
	if err != nil || !found || got.DerivationKey != d.DerivationKey {
		t.Fatalf("got=%#v found=%v err=%v", got, found, err)
	}
	_, found, err = r.FindOperationDerivation(context.Background(), "missing")
	if err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}
}

func TestStructuredSummaryPersistsInspectionCounts(t *testing.T) {
	r := openStructuredRepository(t)
	pending := structuredDerivation(t, 1, core.LifecyclePending, "", core.CompletenessUnavailable)
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	records := []core.Record{structuredDiagnosticSummaryRecord(pending, core.SeverityError, "a.go"), structuredDiagnosticSummaryRecord(pending, core.SeverityWarning, "b.go"), structuredTestSummaryRecord(pending, core.TestPassed), structuredTestSummaryRecord(pending, core.TestError)}
	if err := r.PutRecords(context.Background(), pending.DerivationKey, records); err != nil {
		t.Fatal(err)
	}
	summary, found, err := r.GetRecordSummary(context.Background(), pending.DerivationKey)
	if err != nil || !found {
		t.Fatalf("summary=%#v found=%v err=%v", summary, found, err)
	}
	if summary.RecordsTotal != 4 || summary.Errors != 1 || summary.Warnings != 1 || summary.Files != 2 || summary.TestPassed != 1 || summary.TestFailed != 1 || summary.Mechanical != 4 {
		t.Fatalf("summary=%#v", summary)
	}
}

func structuredDiagnosticSummaryRecord(d core.Derivation, severity core.Severity, path string) core.Record {
	r := structuredArtifactRecord(d, core.AuthorityMechanical, "tmp")
	r.RecordKind = core.RecordDiagnostic
	r.ArtifactResult = nil
	r.Diagnostic = &core.Diagnostic{Severity: severity, Code: "code", Message: "message", Location: providerSummaryLocation(path)}
	return r
}
func structuredTestSummaryRecord(d core.Derivation, status core.TestStatus) core.Record {
	r := structuredArtifactRecord(d, core.AuthorityMechanical, "tmp")
	r.RecordKind = core.RecordTestCase
	r.ArtifactResult = nil
	r.TestCase = &core.TestCase{Name: "Test", Package: "example", Status: status}
	return r
}

var _ structuredapp.InspectionRepository = (*Repository)(nil)

func providerSummaryLocation(path string) source.SourceLocation {
	return source.SourceLocation{Kind: source.LocationProviderReported, ProviderReported: &source.ProviderReportedLocation{Origin: source.OriginRepository, SanitizedLogicalPath: path, Line: 1, Column: 1, NormalizationQuality: source.NormalizationPartial}}
}
