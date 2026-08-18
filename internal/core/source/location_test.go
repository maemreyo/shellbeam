package source

import "testing"

func TestSourceLocationClosedUnionAndPathSafety(t *testing.T) {
	valid := []SourceLocation{
		{Kind: LocationProviderReported, ProviderReported: &ProviderReportedLocation{Origin: OriginRepository, SanitizedLogicalPath: "internal/app/main.go", Line: 5, Column: 2, EndLine: 5, EndColumn: 8, NormalizationQuality: NormalizationPartial}},
		{Kind: LocationResolved, Resolved: &ResolvedSourceLocation{SourceRefID: "src_01K00000000000000000000000", StartByte: 10, EndByte: 20}},
	}
	for i, got := range valid {
		if err := got.Validate(); err != nil {
			t.Fatalf("valid[%d]: %v", i, err)
		}
	}
	invalid := []SourceLocation{
		{},
		{Kind: LocationResolved, ProviderReported: &ProviderReportedLocation{Origin: OriginRepository, SanitizedLogicalPath: "a.go"}},
		{Kind: LocationProviderReported, ProviderReported: &ProviderReportedLocation{Origin: OriginRepository, SanitizedLogicalPath: "/private/a.go", Line: 1, Column: 1, NormalizationQuality: NormalizationPartial}},
		{Kind: LocationProviderReported, ProviderReported: &ProviderReportedLocation{Origin: OriginRepository, SanitizedLogicalPath: "../escape.go", Line: 1, Column: 1, NormalizationQuality: NormalizationPartial}},
		{Kind: LocationResolved, Resolved: &ResolvedSourceLocation{SourceRefID: "src_x", StartByte: 20, EndByte: 10}},
	}
	for i, got := range invalid {
		if err := got.Validate(); err == nil {
			t.Fatalf("invalid[%d] accepted: %#v", i, got)
		}
	}
}

func TestResolvedSourceLocationAcceptsBoundedDisplayNavigation(t *testing.T) {
	location := ResolvedSourceLocation{
		SourceRefID: "src_01K00000000000000000000000", StartByte: 10, EndByte: 20,
		Display: &DisplaySourceLocation{Path: "internal/app/main.go", Line: 5, Column: 2, EndLine: 5, EndColumn: 8, Preview: "\treturn value"},
	}
	if err := location.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := location
	bad.Display = &DisplaySourceLocation{Path: "../private.go", Line: 1, Column: 1, Preview: "x"}
	if err := bad.Validate(); err == nil {
		t.Fatal("unsafe display path accepted")
	}
}
