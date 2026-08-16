package checkpoint

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	testWorkspaceID  = "ws_01K00000000000000000000000"
	testCheckpointID = "chk_01K00000000000000000000000"
	testGeneration   = "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestE26CreateRequestNormalizesSelectorsAndFingerprintStable(t *testing.T) {
	first := CreateRequest{CreateID: "cp-create-1", WorkspaceID: testWorkspaceID, ActivityID: "PI-756", Paths: []string{"tests/runtime/**", "internal/runtime/file.go"}}
	normalized, err := first.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/runtime/file.go", "tests/runtime/**"}
	if !reflect.DeepEqual(normalized.Paths, want) {
		t.Fatalf("paths=%v want=%v", normalized.Paths, want)
	}
	second := first
	second.Paths = []string{"internal/runtime/file.go", "tests/runtime/**"}
	left, err := first.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if left != right || len(left) != 64 {
		t.Fatalf("fingerprints left=%q right=%q", left, right)
	}
	duplicate := first
	duplicate.Paths = []string{"internal/runtime/file.go", "internal/runtime/file.go"}
	if _, err := duplicate.Normalize(); err == nil {
		t.Fatal("duplicate selector accepted")
	}
}

func TestE26CheckpointPublicMetadataIsBoundedAndPrivacySafe(t *testing.T) {
	checkpoint := Checkpoint{
		SchemaVersion: SchemaVersion, CheckpointID: testCheckpointID, CreateID: "cp-create-1",
		Provider: ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: testWorkspaceID, ActivityID: "PI-756",
		SourceGeneration: testGeneration, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		CapturedPathCount: 2, TotalBytes: 17, CaptureQuality: CaptureComplete, RetentionState: RetentionAvailable,
		OpaqueEntryRefs: []string{"entry_01K00000000000000000000000", "entry_01K00000000000000000000001"},
		Excluded:        []PathSummary{{Path: ".git", Reason: "policy_excluded"}},
	}
	if err := checkpoint.Validate(); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"private_content_identity", "content_hash", "workspace_root", "symlink_text", "raw_bytes"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public checkpoint leaked forbidden field %q: %s", forbidden, text)
		}
	}
}

func TestE26RestoreRequestFingerprintIgnoresCallerOrderButRejectsDuplicates(t *testing.T) {
	first := RestoreRequest{RestoreID: "restore-1", CheckpointID: testCheckpointID, Paths: []string{"tests/runtime/file_test.go", "internal/runtime/file.go"}}
	normalized, err := first.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/runtime/file.go", "tests/runtime/file_test.go"}
	if !reflect.DeepEqual(normalized.Paths, want) {
		t.Fatalf("paths=%v want=%v", normalized.Paths, want)
	}
	second := first
	second.Paths = []string{"internal/runtime/file.go", "tests/runtime/file_test.go"}
	left, err := first.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if left != right || len(left) != 64 {
		t.Fatalf("fingerprints left=%q right=%q", left, right)
	}
	duplicate := first
	duplicate.Paths = []string{"internal/runtime/file.go", "internal/runtime/file.go"}
	if _, err := duplicate.Normalize(); err == nil {
		t.Fatal("duplicate restore path accepted")
	}
}

func TestE26RestoreResultCompletenessRequiresEveryRequestedPathSatisfied(t *testing.T) {
	base := RestoreResult{
		SchemaVersion: SchemaVersion, RestoreID: "restore-1", CheckpointID: testCheckpointID,
		Paths:    []RestorePathResult{{Path: "internal/runtime/file.go", Outcome: RestoreRestored}, {Path: "tests/runtime/file_test.go", Outcome: RestoreNoop}},
		Complete: true,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid complete result rejected: %v", err)
	}
	conflict := base
	conflict.Paths = append([]RestorePathResult(nil), base.Paths...)
	conflict.Paths[1].Outcome = RestoreConflict
	if err := conflict.Validate(); err == nil {
		t.Fatal("complete result accepted with conflict")
	}
	conflict.Complete = false
	if err := conflict.Validate(); err != nil {
		t.Fatalf("partial conflict result rejected: %v", err)
	}
	duplicate := base
	duplicate.Paths = []RestorePathResult{{Path: "internal/runtime/file.go", Outcome: RestoreRestored}, {Path: "internal/runtime/file.go", Outcome: RestoreNoop}}
	if err := duplicate.Validate(); err == nil {
		t.Fatal("duplicate result path accepted")
	}
}
