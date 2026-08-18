package hermetic

import (
	"strings"
	"testing"

	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestCaptureLimitsV1AreBoundedAndValid(t *testing.T) {
	limits := DefaultCaptureLimits()
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	if limits.MaxPaths < 1000 || limits.MaxPaths > 8192 || limits.MaxFileBytes <= 0 || limits.MaxTotalBytes <= limits.MaxFileBytes || limits.MaxWalkEntries < limits.MaxPaths {
		t.Fatalf("unexpected default capture limits: %#v", limits)
	}
	bad := []CaptureLimits{
		{},
		{MaxPaths: MaxCapturePaths + 1, MaxFileBytes: 1, MaxTotalBytes: 2, MaxWalkEntries: 2},
		{MaxPaths: 1, MaxFileBytes: MaxCaptureFileBytes + 1, MaxTotalBytes: MaxCaptureTotalBytes, MaxWalkEntries: 2},
		{MaxPaths: 2, MaxFileBytes: 2, MaxTotalBytes: 1, MaxWalkEntries: 2},
		{MaxPaths: 2, MaxFileBytes: 1, MaxTotalBytes: 2, MaxWalkEntries: 1},
	}
	for _, limits := range bad {
		if err := limits.Validate(); err == nil {
			t.Fatalf("accepted invalid limits: %#v", limits)
		}
	}
}

func TestCaptureManifestV1IsCanonicalBoundedAndDigestStable(t *testing.T) {
	manifest := CaptureManifest{
		SchemaVersion:    CaptureManifestSchemaVersion,
		WorkspaceID:      workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		SourceGeneration: "gen_" + strings.Repeat("a", 64),
		Selectors:        []string{"internal/**", "go.mod"},
		Entries: []CaptureEntry{
			{Path: "internal/b.go", Size: 2, SHA256: strings.Repeat("b", 64), Executable: false},
			{Path: "go.mod", Size: 1, SHA256: strings.Repeat("c", 64), Executable: false},
		},
		TotalBytes: 3,
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Entries[0].Path != "go.mod" || canonical.Entries[1].Path != "internal/b.go" {
		t.Fatalf("entries not canonical: %#v", canonical.Entries)
	}
	first, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonical.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("digest first=%q second=%q", first, second)
	}
	mutated := manifest
	mutated.Entries = append([]CaptureEntry(nil), manifest.Entries...)
	mutated.Entries[0].SHA256 = strings.Repeat("d", 64)
	third, err := mutated.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("capture content change did not change manifest digest")
	}
}

func TestCaptureManifestRejectsDuplicatesBadAccountingAndUnsafePaths(t *testing.T) {
	base := CaptureManifest{
		SchemaVersion:    CaptureManifestSchemaVersion,
		WorkspaceID:      workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		SourceGeneration: "gen_" + strings.Repeat("a", 64),
		Entries:          []CaptureEntry{{Path: "go.mod", Size: 1, SHA256: strings.Repeat("b", 64)}},
		TotalBytes:       1,
	}
	cases := []CaptureManifest{
		func() CaptureManifest { x := base; x.SourceGeneration = "bad"; return x }(),
		func() CaptureManifest { x := base; x.TotalBytes = 2; return x }(),
		func() CaptureManifest {
			x := base
			x.Entries = append(x.Entries, x.Entries[0])
			x.TotalBytes = 2
			return x
		}(),
		func() CaptureManifest {
			x := base
			x.Entries = append([]CaptureEntry(nil), x.Entries...)
			x.Entries[0].Path = "../secret"
			return x
		}(),
		func() CaptureManifest {
			x := base
			x.Entries = append([]CaptureEntry(nil), x.Entries...)
			x.Entries[0].SHA256 = "bad"
			return x
		}(),
	}
	for _, manifest := range cases {
		if err := manifest.Validate(); err == nil {
			t.Fatalf("accepted invalid manifest: %#v", manifest)
		}
	}
}
