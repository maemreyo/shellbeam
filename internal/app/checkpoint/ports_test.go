package checkpoint

import (
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

const (
	testWorkspaceID  = "ws_01K00000000000000000000000"
	testCheckpointID = "chk_01K00000000000000000000000"
	testGeneration   = "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestE26CreateReservationValidatesFrozenIdentityAndCanonicalPaths(t *testing.T) {
	now := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	request := core.CreateRequest{CreateID: "cp-create-1", WorkspaceID: testWorkspaceID, ActivityID: "PI-756", Paths: []string{"internal/runtime/file.go", "tests/runtime/**"}}
	fingerprint, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	valid := CreateReservation{
		SchemaVersion:      ReservationSchemaVersion,
		CreateID:           "cp-create-1",
		RequestFingerprint: fingerprint,
		CheckpointID:       testCheckpointID,
		Provider:           core.ProviderIdentity{ID: "localfs", Version: 1},
		WorkspaceID:        testWorkspaceID,
		ActivityID:         "PI-756",
		Paths:              []string{"internal/runtime/file.go", "tests/runtime/**"},
		CreatedAt:          now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid reservation rejected: %v", err)
	}

	unsorted := valid
	unsorted.Paths = []string{"tests/runtime/**", "internal/runtime/file.go"}
	if err := unsorted.Validate(); err == nil {
		t.Fatal("non-canonical reservation paths accepted")
	}

	badFingerprint := valid
	badFingerprint.RequestFingerprint = "not-a-digest"
	if err := badFingerprint.Validate(); err == nil {
		t.Fatal("invalid request fingerprint accepted")
	}

	badGeneration := valid
	badGeneration.SourceGeneration = "gen_bad"
	if err := badGeneration.Validate(); err == nil {
		t.Fatal("invalid bound source generation accepted")
	}

	mismatched := valid
	mismatched.ActivityID = "PI-757"
	if err := mismatched.Validate(); err == nil {
		t.Fatal("reservation accepted fingerprint for different frozen fields")
	}

	bound := valid
	bound.SourceGeneration = testGeneration
	if err := bound.Validate(); err != nil {
		t.Fatalf("valid bound source rejected: %v", err)
	}
}

func TestE26RestoreReservationValidatesExactCanonicalPaths(t *testing.T) {
	now := time.Date(2026, 8, 16, 8, 1, 0, 0, time.UTC)
	request := core.RestoreRequest{RestoreID: "restore-1", CheckpointID: testCheckpointID, Paths: []string{"internal/runtime/file.go", "tests/runtime/file_test.go"}}
	fingerprint, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	valid := RestoreReservation{
		SchemaVersion:      ReservationSchemaVersion,
		RestoreID:          "restore-1",
		RequestFingerprint: fingerprint,
		CheckpointID:       testCheckpointID,
		WorkspaceID:        testWorkspaceID,
		Paths:              []string{"internal/runtime/file.go", "tests/runtime/file_test.go"},
		StartedAt:          now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid restore reservation rejected: %v", err)
	}

	unsorted := valid
	unsorted.Paths = []string{"tests/runtime/file_test.go", "internal/runtime/file.go"}
	if err := unsorted.Validate(); err == nil {
		t.Fatal("non-canonical restore paths accepted")
	}

	glob := valid
	glob.Paths = []string{"internal/runtime/**"}
	if err := glob.Validate(); err == nil {
		t.Fatal("restore glob accepted")
	}

	mismatched := valid
	mismatched.Paths = []string{"internal/runtime/other.go", "tests/runtime/file_test.go"}
	if err := mismatched.Validate(); err == nil {
		t.Fatal("restore reservation accepted fingerprint for different frozen paths")
	}

	badWorkspace := valid
	badWorkspace.WorkspaceID = "bad"
	if err := badWorkspace.Validate(); err == nil {
		t.Fatal("invalid restore workspace accepted")
	}
}
