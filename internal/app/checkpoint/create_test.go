package checkpoint

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestCheckpointCreateReplaysCompletedBeforeWorkspaceProviderOrIDAllocation(t *testing.T) {
	request := createTestRequest()
	reservation, checkpoint := createCompletedReservation(t, request)
	calls := []string{}
	repo := &createTestRepository{calls: &calls, reservation: &reservation, checkpoint: &checkpoint}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{createWorkspaceContext("/moved/root", "gen_"+repeatHex("b"))}}
	provider := &createTestProvider{calls: &calls, identity: reservation.Provider}
	svc := New(repo, workspace, provider)
	ids := 0
	svc.newCheckpointID = func() string { ids++; calls = append(calls, "new_id"); return "chk_01K11111111111111111111111" }

	got, err := svc.Create(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, checkpoint) {
		t.Fatalf("replay=%#v err=%v", got, err)
	}
	if !reflect.DeepEqual(calls, []string{"find"}) || ids != 0 || workspace.resolveCalls != 0 || len(provider.captures) != 0 {
		t.Fatalf("replay performed mutable work calls=%v ids=%d resolves=%d captures=%d", calls, ids, workspace.resolveCalls, len(provider.captures))
	}
}

func TestCheckpointCreateConflictStopsBeforeWorkspaceProviderOrIDAllocation(t *testing.T) {
	original := createTestRequest()
	reservation, _ := createCompletedReservation(t, original)
	calls := []string{}
	repo := &createTestRepository{calls: &calls, reservation: &reservation}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{createWorkspaceContext("/repo", testGeneration)}}
	provider := &createTestProvider{calls: &calls, identity: reservation.Provider}
	svc := New(repo, workspace, provider)
	ids := 0
	svc.newCheckpointID = func() string { ids++; return "chk_01K11111111111111111111111" }
	conflict := original
	conflict.Paths = []string{"internal/runtime/other.go", "tests/runtime/**"}

	_, err := svc.Create(context.Background(), conflict)
	if !checkpointFailureIs(err, failure.CheckpointCreateConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	if !reflect.DeepEqual(calls, []string{"find"}) || ids != 0 || workspace.resolveCalls != 0 || len(provider.captures) != 0 {
		t.Fatalf("conflict performed work calls=%v ids=%d resolves=%d captures=%d", calls, ids, workspace.resolveCalls, len(provider.captures))
	}
}

func TestCheckpointCreateFreezesReservationBeforeFreshObservationAndCapture(t *testing.T) {
	calls := []string{}
	repo := &createTestRepository{calls: &calls}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{createWorkspaceContext("/repo", testGeneration)}}
	provider := &createTestProvider{calls: &calls, identity: core.ProviderIdentity{ID: "localfs", Version: 1}, results: []CaptureResult{createCaptureResult()}}
	svc := New(repo, workspace, provider)
	svc.now = func() time.Time { return time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC) }
	svc.newCheckpointID = func() string { calls = append(calls, "new_id"); return testCheckpointID }

	got, err := svc.Create(context.Background(), createTestRequest())
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"find", "provider.identity", "new_id", "reserve", "workspace.resolve", "bind_source", "provider.capture", "complete"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("call order=%v want=%v", calls, wantCalls)
	}
	if repo.reservation == nil || repo.reservation.Provider != provider.identity || repo.reservation.SourceGeneration != testGeneration || repo.reservation.CheckpointID != testCheckpointID {
		t.Fatalf("reservation not frozen/bound: %#v", repo.reservation)
	}
	if len(provider.captures) != 1 {
		t.Fatalf("capture calls=%d", len(provider.captures))
	}
	capture := provider.captures[0]
	if capture.CheckpointID != testCheckpointID || capture.WorkspaceID != testWorkspaceID || capture.Root != "/repo" || capture.SourceGeneration != testGeneration || !reflect.DeepEqual(capture.Paths, []string{"internal/runtime/file.go", "tests/runtime/**"}) {
		t.Fatalf("capture request=%#v", capture)
	}
	if got.CheckpointID != testCheckpointID || got.SourceGeneration != testGeneration || got.Provider != provider.identity || got.RetentionState != core.RetentionAvailable {
		t.Fatalf("checkpoint=%#v", got)
	}
}

func TestCheckpointCreateProviderUnavailableOnlyAfterReplayLookup(t *testing.T) {
	calls := []string{}
	repo := &createTestRepository{calls: &calls}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{createWorkspaceContext("/repo", testGeneration)}}
	svc := New(repo, workspace, nil)
	ids := 0
	svc.newCheckpointID = func() string { ids++; return testCheckpointID }

	_, err := svc.Create(context.Background(), createTestRequest())
	if !checkpointFailureIs(err, failure.CheckpointProviderUnavailable) {
		t.Fatalf("provider unavailable err=%v", err)
	}
	if !reflect.DeepEqual(calls, []string{"find"}) || ids != 0 || workspace.resolveCalls != 0 {
		t.Fatalf("provider unavailable ordering calls=%v ids=%d resolves=%d", calls, ids, workspace.resolveCalls)
	}
}

func TestCheckpointCreateRequiresFreshGenerationBeforeCaptureAndCanResumeSameID(t *testing.T) {
	calls := []string{}
	repo := &createTestRepository{calls: &calls}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{
		createWorkspaceContext("/repo", ""),
		createWorkspaceContext("/repo", testGeneration),
	}}
	provider := &createTestProvider{calls: &calls, identity: core.ProviderIdentity{ID: "localfs", Version: 1}, results: []CaptureResult{createCaptureResult()}}
	svc := New(repo, workspace, provider)
	idCalls := 0
	svc.newCheckpointID = func() string { idCalls++; calls = append(calls, "new_id"); return testCheckpointID }

	_, err := svc.Create(context.Background(), createTestRequest())
	if !checkpointFailureIs(err, failure.CheckpointScopeInvalid) || len(provider.captures) != 0 || repo.completeCalls != 0 {
		t.Fatalf("missing generation err=%v captures=%d completes=%d", err, len(provider.captures), repo.completeCalls)
	}
	got, err := svc.Create(context.Background(), createTestRequest())
	if err != nil || got.CheckpointID != testCheckpointID {
		t.Fatalf("resume got=%#v err=%v", got, err)
	}
	if idCalls != 1 || len(provider.captures) != 1 || provider.captures[0].CheckpointID != testCheckpointID {
		t.Fatalf("resume allocated/recaptured incorrectly ids=%d captures=%#v", idCalls, provider.captures)
	}
}

func TestCheckpointCreateCaptureFailureRetainsDurableIDAndProviderIdentity(t *testing.T) {
	calls := []string{}
	repo := &createTestRepository{calls: &calls}
	workspace := &createTestWorkspace{calls: &calls, contexts: []WorkspaceContext{createWorkspaceContext("/repo", testGeneration)}}
	provider := &createTestProvider{
		calls:    &calls,
		identity: core.ProviderIdentity{ID: "localfs", Version: 1},
		errs:     []error{errors.New("capture failed")},
		results:  []CaptureResult{{}, createCaptureResult()},
	}
	svc := New(repo, workspace, provider)
	idCalls := 0
	svc.newCheckpointID = func() string { idCalls++; return testCheckpointID }

	if _, err := svc.Create(context.Background(), createTestRequest()); err == nil {
		t.Fatal("capture failure accepted")
	}
	if repo.reservation == nil || repo.reservation.CheckpointID != testCheckpointID || repo.checkpoint != nil || idCalls != 1 {
		t.Fatalf("failure did not retain reservation=%#v checkpoint=%#v ids=%d", repo.reservation, repo.checkpoint, idCalls)
	}
	provider.errs = nil
	provider.results = []CaptureResult{{}, createCaptureResult()}
	got, err := svc.Create(context.Background(), createTestRequest())
	if err != nil || got.CheckpointID != testCheckpointID || idCalls != 1 || len(provider.captures) != 2 {
		t.Fatalf("retry got=%#v err=%v ids=%d captures=%d", got, err, idCalls, len(provider.captures))
	}
	if provider.captures[0].CheckpointID != provider.captures[1].CheckpointID {
		t.Fatalf("retry changed checkpoint id: %#v", provider.captures)
	}

	// A different provider version cannot continue the frozen reservation.
	repo.checkpoint = nil
	provider.identity = core.ProviderIdentity{ID: "localfs", Version: 2}
	_, err = svc.Create(context.Background(), createTestRequest())
	if !checkpointFailureIs(err, failure.CheckpointProviderUnavailable) {
		t.Fatalf("provider identity drift err=%v", err)
	}
}

func TestCheckpointCreateRejectsOversizedProviderResultBeforeMetadataPublish(t *testing.T) {
	repo := &createTestRepository{}
	workspace := &createTestWorkspace{contexts: []WorkspaceContext{createWorkspaceContext("/repo", testGeneration)}}
	oversized := createCaptureResult()
	oversized.TotalBytes = core.MaxCheckpointBytes + 1
	provider := &createTestProvider{identity: core.ProviderIdentity{ID: "localfs", Version: 1}, results: []CaptureResult{oversized}}
	svc := New(repo, workspace, provider)
	svc.newCheckpointID = func() string { return testCheckpointID }

	_, err := svc.Create(context.Background(), createTestRequest())
	if !checkpointFailureIs(err, failure.CheckpointBudgetExceeded) || repo.completeCalls != 0 || repo.checkpoint != nil {
		t.Fatalf("oversized provider result err=%v completes=%d checkpoint=%#v", err, repo.completeCalls, repo.checkpoint)
	}
}

func repeatHex(ch string) string { return strings.Repeat(ch, 64) }
