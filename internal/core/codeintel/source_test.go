package codeintel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestSourceRefValidatesOpaqueIdentityAndSafeMetadata(t *testing.T) {
	ref := SourceRef{
		ID:                SourceRefID("src_01K00000000000000000000000"),
		Origin:            SourceWorkspace,
		RepositoryID:      workspace.RepositoryID("repo_01K00000000000000000000000"),
		WorkspaceID:       workspace.WorkspaceID("ws_01K00000000000000000000000"),
		LogicalPath:       "internal/app/service.go",
		DisplayIdentity:   "internal/app/service.go",
		ResolutionQuality: ResolutionExact,
		TextEncoding:      TextEncodingUTF8,
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("valid source ref rejected: %v", err)
	}

	encoded, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	for _, want := range []string{`"source_ref_id":"src_`, `"origin":"workspace"`, `"resolution_quality":"exact"`, `"text_encoding":"utf-8"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("encoded source ref missing %s: %s", want, got)
		}
	}
}

func TestSourceRefRejectsRebindUnsafeOrNonUTF8Metadata(t *testing.T) {
	valid := SourceRef{
		ID:                SourceRefID("src_01K00000000000000000000000"),
		Origin:            SourceRepository,
		RepositoryID:      workspace.RepositoryID("repo_01K00000000000000000000000"),
		LogicalPath:       "main.go",
		ResolutionQuality: ResolutionObserved,
		TextEncoding:      TextEncodingUTF8,
	}

	cases := map[string]SourceRef{
		"short opaque id":       func() SourceRef { v := valid; v.ID = "src_x"; return v }(),
		"unknown origin":        func() SourceRef { v := valid; v.Origin = "mystery"; return v }(),
		"invalid repository id": func() SourceRef { v := valid; v.RepositoryID = "repo_x"; return v }(),
		"invalid workspace id":  func() SourceRef { v := valid; v.WorkspaceID = "ws_x"; return v }(),
		"absolute logical path": func() SourceRef { v := valid; v.LogicalPath = "/private/main.go"; return v }(),
		"escaping logical path": func() SourceRef { v := valid; v.LogicalPath = "../main.go"; return v }(),
		"control display text":  func() SourceRef { v := valid; v.DisplayIdentity = "main.go\nsecret"; return v }(),
		"oversized display text": func() SourceRef {
			v := valid
			v.DisplayIdentity = strings.Repeat("x", MaxDisplayIdentityBytes+1)
			return v
		}(),
		"unknown quality":   func() SourceRef { v := valid; v.ResolutionQuality = "fresh"; return v }(),
		"non utf8 encoding": func() SourceRef { v := valid; v.TextEncoding = "utf-16"; return v }(),
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ref.Validate(); err == nil {
				t.Fatalf("invalid source ref accepted: %#v", ref)
			}
		})
	}
}

func TestSourceRefIDParserAcceptsOnlyOpaqueULIDForm(t *testing.T) {
	const raw = "src_01K00000000000000000000000"
	got, err := ParseSourceRefID(raw)
	if err != nil {
		t.Fatalf("valid id rejected: %v", err)
	}
	if got != SourceRefID(raw) {
		t.Fatalf("parsed id = %q", got)
	}
	for _, invalid := range []string{"", "src_x", "SRC_01K00000000000000000000000", "src_01I00000000000000000000000", raw + "x"} {
		if _, err := ParseSourceRefID(invalid); err == nil {
			t.Fatalf("invalid source ref id accepted: %q", invalid)
		}
	}
}

func TestSourceLocationUsesCanonicalClosedUnionAndHalfOpenRanges(t *testing.T) {
	valid := []SourceLocation{
		{
			Kind: LocationResolved,
			Resolved: &ResolvedSourceLocation{
				SourceRefID: "src_01K00000000000000000000000",
				StartByte:   7,
				EndByte:     7,
			},
		},
		{
			Kind: LocationProviderReported,
			ProviderReported: &ProviderReportedLocation{
				Origin:               OriginDependency,
				SanitizedLogicalPath: "example.com/mod/pkg/file.go",
				Line:                 4,
				Column:               2,
				EndLine:              4,
				EndColumn:            5,
				NormalizationQuality: NormalizationPartial,
			},
		},
	}
	for i, location := range valid {
		if err := location.Validate(); err != nil {
			t.Fatalf("valid[%d] rejected: %v", i, err)
		}
	}

	invalid := []SourceLocation{
		{},
		{Kind: LocationResolved, Resolved: &ResolvedSourceLocation{SourceRefID: "src_01K00000000000000000000000", StartByte: 8, EndByte: 7}},
		{Kind: LocationResolved, Resolved: &ResolvedSourceLocation{SourceRefID: "src_01K00000000000000000000000", StartByte: 0, EndByte: 1}, ProviderReported: &ProviderReportedLocation{}},
	}
	for i, location := range invalid {
		if err := location.Validate(); err == nil {
			t.Fatalf("invalid[%d] accepted: %#v", i, location)
		}
	}
}
