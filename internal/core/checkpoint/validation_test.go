package checkpoint

import (
	"strings"
	"testing"
	"time"
)

func TestE26CreateSelectorGrammarFailsClosed(t *testing.T) {
	valid := CreateRequest{CreateID: "cp-create-1", WorkspaceID: testWorkspaceID, Paths: []string{"internal/runtime/file.go"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	invalidPaths := []string{"", "**", "/absolute", "../escape", "a/../b", "./a", "a//b", `a\\b`, "a/*", "a/**/b", "a?", "a[0]", "a{b}", "a/", strings.Repeat("x", MaxSelectorBytes+1)}
	for _, path := range invalidPaths {
		request := valid
		request.Paths = []string{path}
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid selector accepted: %q", path)
		}
	}
	tooMany := valid
	tooMany.Paths = make([]string, MaxCreateSelectors+1)
	for i := range tooMany.Paths {
		tooMany.Paths[i] = "p" + strings.Repeat("x", i+1)
	}
	if err := tooMany.Validate(); err == nil {
		t.Fatal("selector count limit not enforced")
	}
	tooLarge := valid
	tooLarge.Paths = make([]string, 9)
	for i := range tooLarge.Paths {
		tooLarge.Paths[i] = string(rune('a'+i)) + strings.Repeat("x", 1022)
	}
	if err := tooLarge.Validate(); err == nil {
		t.Fatal("total selector byte limit not enforced")
	}
}

func TestE26RestorePathsAreExactAndBounded(t *testing.T) {
	valid := RestoreRequest{RestoreID: "restore-1", CheckpointID: testCheckpointID, Paths: []string{"internal/runtime/file.go"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid restore rejected: %v", err)
	}
	for _, path := range []string{"", "**", "internal/**", "/absolute", "../escape", `a\\b`, "a/*"} {
		request := valid
		request.Paths = []string{path}
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid restore path accepted: %q", path)
		}
	}
	tooMany := valid
	tooMany.Paths = make([]string, MaxRestorePaths+1)
	for i := range tooMany.Paths {
		tooMany.Paths[i] = "restore/path/" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	if err := tooMany.Validate(); err == nil {
		t.Fatal("restore path count limit not enforced")
	}
}

func TestE26ProviderAndConflictVocabularyIsClosed(t *testing.T) {
	valid := ProviderIdentity{ID: "localfs", Version: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ProviderIdentity{{}, {ID: "LocalFS", Version: 1}, {ID: "local/fs", Version: 1}, {ID: "localfs", Version: 0}} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid provider accepted: %#v", bad)
		}
	}
	matrix := ConflictDetection{RegularFile: ConflictBestEffort, Symlink: ConflictBestEffort, AbsentToFile: ConflictBestEffort, DirectoryTree: ConflictUnsupported}
	if err := matrix.Validate(); err != nil {
		t.Fatalf("valid conflict matrix rejected: %v", err)
	}
	matrix.DirectoryTree = ConflictGuarantee("probably_atomic")
	if err := matrix.Validate(); err == nil {
		t.Fatal("unknown conflict guarantee accepted")
	}
}

func TestE26CheckpointValidationRejectsInvalidStateAndBudgets(t *testing.T) {
	base := Checkpoint{
		SchemaVersion: SchemaVersion, CheckpointID: testCheckpointID, CreateID: "cp-create-1",
		Provider: ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: testWorkspaceID, SourceGeneration: testGeneration,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(), CapturedPathCount: 1, TotalBytes: 1,
		CaptureQuality: CaptureComplete, RetentionState: RetentionAvailable,
		OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Checkpoint)
	}{
		{"schema", func(c *Checkpoint) { c.SchemaVersion = 0 }},
		{"checkpoint id", func(c *Checkpoint) { c.CheckpointID = "bad" }},
		{"create id", func(c *Checkpoint) { c.CreateID = "../bad" }},
		{"workspace", func(c *Checkpoint) { c.WorkspaceID = "bad" }},
		{"generation", func(c *Checkpoint) { c.SourceGeneration = "gen_bad" }},
		{"capture quality", func(c *Checkpoint) { c.CaptureQuality = CaptureQuality("partial") }},
		{"retention", func(c *Checkpoint) { c.RetentionState = RetentionState("maybe") }},
		{"negative bytes", func(c *Checkpoint) { c.TotalBytes = -1 }},
		{"entry refs", func(c *Checkpoint) { c.OpaqueEntryRefs = make([]string, MaxPublicEntryRefs+1) }},
		{"summaries", func(c *Checkpoint) { c.Excluded = make([]PathSummary, MaxPublicSummaries+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base
			got.OpaqueEntryRefs = append([]string(nil), base.OpaqueEntryRefs...)
			tc.mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid checkpoint accepted: %#v", got)
			}
		})
	}
}
