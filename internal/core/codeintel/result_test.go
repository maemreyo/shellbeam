package codeintel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestResultValidateAcceptsQueryCompatibleRecords(t *testing.T) {
	location := resolvedTestLocation()
	selection := SelectionMetadata{
		Basis:          workspace.SelectionWorkspaceDirty,
		Freshness:      workspace.SampleFreshlySampled,
		Completeness:   workspace.SelectionComplete,
		ManagedOverlap: false,
	}
	cases := []struct {
		name   string
		query  Query
		record Record
	}{
		{"diagnostic", Query{Kind: QueryDiagnostics, Scope: ScopeFile, Path: "main.go"}, diagnosticTestRecord(location)},
		{"symbol", Query{Kind: QuerySymbols, Scope: ScopeFile, Path: "main.go"}, symbolTestRecord(location)},
		{"definition", Query{Kind: QueryDefinition, Path: "main.go", Line: 1, Column: 1}, definitionTestRecord(location)},
		{"import declaration", Query{Kind: QueryImportDeclarations, Scope: ScopeFile, Path: "main.go"}, importTestRecord(location)},
		{"type summary", Query{Kind: QueryTypeSummary, Path: "main.go", Line: 1, Column: 1}, typeSummaryTestRecord(location)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Result{
				Status:    StatusReady,
				Query:     tc.query,
				Selection: selection,
				Provider:  testProviderMetadata(),
				Records:   []Record{tc.record},
			}
			if err := result.Validate(testResultLimits()); err != nil {
				t.Fatalf("valid result rejected: %v", err)
			}
		})
	}
}

func diagnosticTestRecord(location SourceLocation) Record {
	return Record{
		Kind: RecordDiagnostic, Authority: AuthorityMechanical, SourceCorrelation: CorrelationCurrent,
		Diagnostic: &Diagnostic{
			Severity: SeverityError, Code: "UndeclaredName",
			Message: "undefined: ServerInfo", Location: location,
		},
	}
}

func symbolTestRecord(location SourceLocation) Record {
	return Record{
		Kind: RecordSymbol, Authority: AuthorityMechanical, SourceCorrelation: CorrelationCurrent,
		Symbol: &Symbol{Name: "ServerInfo", Kind: "struct", Location: location},
	}
}

func definitionTestRecord(location SourceLocation) Record {
	return Record{
		Kind: RecordLocationTarget, Authority: AuthorityMechanical, SourceCorrelation: CorrelationCurrent,
		LocationTarget: &LocationTarget{Relationship: "definition", Location: location},
	}
}

func importTestRecord(location SourceLocation) Record {
	return Record{
		Kind: RecordImport, Authority: AuthorityMechanical, SourceCorrelation: CorrelationCurrent,
		Import: &ImportRecord{Declaration: "fmt", Location: location},
	}
}

func typeSummaryTestRecord(location SourceLocation) Record {
	return Record{
		Kind: RecordTypeSummary, Authority: AuthorityMechanical, SourceCorrelation: CorrelationCurrent,
		TypeSummary: &TypeSummary{Text: "type ServerInfo struct", Location: &location},
	}
}

func TestResultValidateRejectsWrongBranchAndBounds(t *testing.T) {
	location := resolvedTestLocation()
	base := Result{
		Status:   StatusReady,
		Query:    Query{Kind: QueryDiagnostics, Scope: ScopeFile, Path: "main.go"},
		Provider: testProviderMetadata(),
		Records: []Record{{
			Kind:              RecordDiagnostic,
			Authority:         AuthorityMechanical,
			SourceCorrelation: CorrelationCurrent,
			Diagnostic: &Diagnostic{
				Severity: SeverityWarning,
				Message:  "warning",
				Location: location,
			},
		}},
	}
	if err := base.Validate(testResultLimits()); err != nil {
		t.Fatalf("baseline invalid: %v", err)
	}

	wrongQuery := base
	wrongQuery.Query = Query{Kind: QuerySymbols, Scope: ScopeFile, Path: "main.go"}
	if err := wrongQuery.Validate(testResultLimits()); err == nil {
		t.Fatal("diagnostic record accepted for symbols query")
	}

	twoBranches := base
	twoBranches.Records = []Record{{
		Kind:              RecordDiagnostic,
		Authority:         AuthorityMechanical,
		SourceCorrelation: CorrelationCurrent,
		Diagnostic:        base.Records[0].Diagnostic,
		Symbol:            &Symbol{Name: "x", Kind: "variable", Location: location},
	}}
	if err := twoBranches.Validate(testResultLimits()); err == nil {
		t.Fatal("record with two union branches accepted")
	}

	tooMany := base
	tooMany.Records = append(append([]Record{}, base.Records...), base.Records[0])
	limits := testResultLimits()
	limits.MaxRecords = 1
	if err := tooMany.Validate(limits); err == nil {
		t.Fatal("record count budget exceeded without error")
	}

	oversized := base
	copyRecord := *base.Records[0].Diagnostic
	copyRecord.Message = strings.Repeat("x", testResultLimits().MaxTextBytes+1)
	oversized.Records = []Record{{
		Kind:              RecordDiagnostic,
		Authority:         AuthorityMechanical,
		SourceCorrelation: CorrelationCurrent,
		Diagnostic:        &copyRecord,
	}}
	if err := oversized.Validate(testResultLimits()); err == nil {
		t.Fatal("text budget exceeded without error")
	}

	byteLimited := base
	raw, err := json.Marshal(byteLimited)
	if err != nil {
		t.Fatal(err)
	}
	byteLimits := testResultLimits()
	byteLimits.MaxResponseBytes = len(raw) - 1
	if err := byteLimited.Validate(byteLimits); err == nil {
		t.Fatal("response byte budget exceeded without error")
	}
}

func TestCallHierarchyMechanicalAuthorityDoesNotClaimExhaustiveCompleteness(t *testing.T) {
	record := Record{
		Kind:              RecordLocationTarget,
		Authority:         AuthorityMechanical,
		SourceCorrelation: CorrelationCurrent,
		Completeness:      CompletenessExhaustive,
		LocationTarget: &LocationTarget{
			Relationship: "caller",
			Location:     resolvedTestLocation(),
		},
	}
	result := Result{
		Status:   StatusReady,
		Query:    Query{Kind: QueryCallers, Path: "main.go", Line: 1, Column: 1},
		Provider: testProviderMetadata(),
		Records:  []Record{record},
	}
	if err := result.Validate(testResultLimits()); err == nil {
		t.Fatal("mechanical call hierarchy claimed exhaustive completeness")
	}
}

func resolvedTestLocation() SourceLocation {
	return SourceLocation{
		Kind: LocationResolved,
		Resolved: &ResolvedSourceLocation{
			SourceRefID: "src_01K00000000000000000000000",
			StartByte:   0,
			EndByte:     1,
		},
	}
}

func testProviderMetadata() ProviderMetadata {
	return ProviderMetadata{
		ProviderID:   "go_semantic",
		Incarnation:  "provider_01K000000000000000000000000",
		BuildQuality: "observed",
		Coverage:     SyncExactForKnownPaths,
	}
}

func testResultLimits() ResultLimits {
	return ResultLimits{
		MaxRecords:          16,
		MaxResponseBytes:    64 << 10,
		MaxTextBytes:        4096,
		MaxRelatedLocations: 8,
	}
}

func TestUnavailableResultOmitsUnselectedMetadata(t *testing.T) {
	result := Result{Status: StatusUnavailable, Query: Query{Kind: QueryDiagnostics, Scope: ScopeChangedFiles}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	if strings.Contains(got, `"selection"`) || strings.Contains(got, `"provider"`) {
		t.Fatalf("unselected metadata leaked: %s", got)
	}
}
