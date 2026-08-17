package checkpoint

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type restoreTestRepository struct {
	*createTestRepository
	calls              *[]string
	restoreReservation *RestoreReservation
	restorePaths       []core.RestorePathResult
	restoreFinal       *core.RestoreResult
	recorded           []core.RestorePathResult
	completed          *core.RestoreResult
	loadRestoreErr     error
	reserveErr         error
}

func (r *restoreTestRepository) LoadCheckpointRestore(_ context.Context, id string) (RestoreReservation, []core.RestorePathResult, *core.RestoreResult, error) {
	if r.calls != nil {
		*r.calls = append(*r.calls, "load_restore")
	}
	if r.loadRestoreErr != nil {
		return RestoreReservation{}, nil, nil, r.loadRestoreErr
	}
	if r.restoreReservation == nil || r.restoreReservation.RestoreID != id {
		return RestoreReservation{}, nil, nil, ErrRestoreNotFound
	}
	return *r.restoreReservation, append([]core.RestorePathResult(nil), r.restorePaths...), cloneRestoreResult(r.restoreFinal), nil
}

func (r *restoreTestRepository) LoadCheckpoint(ctx context.Context, id string) (core.Checkpoint, error) {
	if r.calls != nil {
		*r.calls = append(*r.calls, "load_checkpoint")
	}
	return r.createTestRepository.LoadCheckpoint(ctx, id)
}

func (r *restoreTestRepository) ReserveCheckpointRestore(_ context.Context, in RestoreReservation) (RestoreReservation, *core.RestoreResult, bool, error) {
	if r.calls != nil {
		*r.calls = append(*r.calls, "reserve_restore")
	}
	if r.reserveErr != nil {
		return RestoreReservation{}, nil, false, r.reserveErr
	}
	if r.restoreReservation != nil {
		if r.restoreReservation.RequestFingerprint != in.RequestFingerprint {
			return RestoreReservation{}, nil, false, failure.New(failure.CheckpointRestoreRequestConflict, map[string]string{"restore_id": in.RestoreID}, nil)
		}
		return *r.restoreReservation, cloneRestoreResult(r.restoreFinal), false, nil
	}
	copy := in
	r.restoreReservation = &copy
	return copy, nil, true, nil
}

func (r *restoreTestRepository) RecordCheckpointRestorePath(_ context.Context, _ string, ordinal int, result core.RestorePathResult) error {
	if r.calls != nil {
		*r.calls = append(*r.calls, "record_"+string(rune('0'+ordinal)))
	}
	r.recorded = append(r.recorded, result)
	return nil
}

func (r *restoreTestRepository) CompleteCheckpointRestore(_ context.Context, _ string, result core.RestoreResult) (core.RestoreResult, error) {
	if r.calls != nil {
		*r.calls = append(*r.calls, "complete_restore")
	}
	copy := result
	r.completed = &copy
	r.restoreFinal = &copy
	return copy, nil
}

type restoreTestWorkspace struct {
	calls       *[]string
	context     WorkspaceContext
	invalidated int
}

func (w *restoreTestWorkspace) ResolveFresh(context.Context, string) (WorkspaceContext, error) {
	if w.calls != nil {
		*w.calls = append(*w.calls, "workspace.resolve")
	}
	return w.context, nil
}

func (w *restoreTestWorkspace) InvalidateAfterMutation(context.Context, string) error {
	if w.calls != nil {
		*w.calls = append(*w.calls, "workspace.invalidate")
	}
	w.invalidated++
	return nil
}

type restoreTestProvider struct {
	*createTestProvider
	calls    *[]string
	restores []ProviderRestoreRequest
	result   ProviderRestoreResult
	err      error
}

func (p *restoreTestProvider) Restore(_ context.Context, req ProviderRestoreRequest) (ProviderRestoreResult, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "provider.restore")
	}
	p.restores = append(p.restores, req)
	return p.result, p.err
}

func TestCheckpointRestoreReplaysCompletedBeforeCheckpointWorkspaceOrProvider(t *testing.T) {
	request := restoreCoreRequest([]string{"a.txt"})
	reservation := restoreReservationFor(t, request)
	final := &core.RestoreResult{
		SchemaVersion: core.SchemaVersion, RestoreID: request.RestoreID, CheckpointID: request.CheckpointID,
		Complete: true, Paths: []core.RestorePathResult{{Path: "a.txt", Outcome: core.RestoreNoop}},
	}
	calls := []string{}
	repo := &restoreTestRepository{
		createTestRepository: &createTestRepository{}, calls: &calls,
		restoreReservation: &reservation, restoreFinal: final,
	}
	workspace := &restoreTestWorkspace{calls: &calls, context: createWorkspaceContext("/repo", testGeneration)}
	provider := &restoreTestProvider{createTestProvider: &createTestProvider{identity: core.ProviderIdentity{ID: "localfs", Version: 1}}, calls: &calls}
	svc := New(repo, workspace, provider)

	got, err := svc.Restore(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, *final) {
		t.Fatalf("replay=%#v err=%v", got, err)
	}
	if !reflect.DeepEqual(calls, []string{"load_restore"}) || len(provider.restores) != 0 || workspace.invalidated != 0 {
		t.Fatalf("completed replay performed extra work calls=%v restores=%d invalidated=%d", calls, len(provider.restores), workspace.invalidated)
	}
}

func TestCheckpointRestoreReservesBeforeProviderPersistsPathsAndInvalidatesOnce(t *testing.T) {
	request := restoreCoreRequest([]string{"a.txt", "b.txt"})
	_, checkpoint := createCompletedReservation(t, createTestRequest())
	calls := []string{}
	base := &createTestRepository{checkpoint: &checkpoint}
	repo := &restoreTestRepository{createTestRepository: base, calls: &calls}
	workspace := &restoreTestWorkspace{calls: &calls, context: createWorkspaceContext("/repo", testGeneration)}
	provider := &restoreTestProvider{
		createTestProvider: &createTestProvider{identity: checkpoint.Provider}, calls: &calls,
		result: ProviderRestoreResult{Paths: []core.RestorePathResult{
			{Path: "a.txt", Outcome: core.RestoreRestored},
			{Path: "b.txt", Outcome: core.RestoreNoop},
		}},
	}
	svc := New(repo, workspace, provider)
	svc.now = func() time.Time { return time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC) }

	got, err := svc.Restore(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || len(got.Paths) != 2 || workspace.invalidated != 1 {
		t.Fatalf("restore result=%#v invalidated=%d", got, workspace.invalidated)
	}
	wantCalls := []string{
		"load_restore", "load_checkpoint", "reserve_restore", "workspace.resolve",
		"provider.restore", "record_0", "record_1", "complete_restore", "workspace.invalidate",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("call order=%v want=%v", calls, wantCalls)
	}
	if len(provider.restores) != 1 || provider.restores[0].WorkspaceID != checkpoint.WorkspaceID || provider.restores[0].Root != "/repo" {
		t.Fatalf("provider request=%#v", provider.restores)
	}
	if !reflect.DeepEqual(repo.recorded, provider.result.Paths) {
		t.Fatalf("durable path truth=%#v provider=%#v", repo.recorded, provider.result.Paths)
	}
}

func TestCheckpointRestorePersistsPartialTruthAndReturnsIncomplete(t *testing.T) {
	request := restoreCoreRequest([]string{"a.txt", "b.txt"})
	_, checkpoint := createCompletedReservation(t, createTestRequest())
	repo := &restoreTestRepository{createTestRepository: &createTestRepository{checkpoint: &checkpoint}}
	workspace := &restoreTestWorkspace{context: createWorkspaceContext("/repo", testGeneration)}
	provider := &restoreTestProvider{
		createTestProvider: &createTestProvider{identity: checkpoint.Provider},
		result: ProviderRestoreResult{Paths: []core.RestorePathResult{
			{Path: "a.txt", Outcome: core.RestoreRestored},
			{Path: "b.txt", Outcome: core.RestoreConflict, Reason: "current_changed"},
		}},
	}
	svc := New(repo, workspace, provider)

	got, err := svc.Restore(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || !reflect.DeepEqual(got.Paths, provider.result.Paths) || repo.completed == nil || repo.completed.Complete {
		t.Fatalf("partial result=%#v durable=%#v", got, repo.completed)
	}
	if workspace.invalidated != 1 {
		t.Fatalf("restored path did not invalidate workspace exactly once: %d", workspace.invalidated)
	}
}

func TestCheckpointRestoreConflictAndExpiredFailBeforeWorkspaceProvider(t *testing.T) {
	t.Run("conflicting replay", func(t *testing.T) {
		original := restoreCoreRequest([]string{"a.txt"})
		reservation := restoreReservationFor(t, original)
		calls := []string{}
		repo := &restoreTestRepository{createTestRepository: &createTestRepository{}, calls: &calls, restoreReservation: &reservation}
		workspace := &restoreTestWorkspace{calls: &calls, context: createWorkspaceContext("/repo", testGeneration)}
		provider := &restoreTestProvider{createTestProvider: &createTestProvider{identity: core.ProviderIdentity{ID: "localfs", Version: 1}}, calls: &calls}
		svc := New(repo, workspace, provider)
		conflict := original
		conflict.Paths = []string{"b.txt"}

		_, err := svc.Restore(context.Background(), conflict)
		if !checkpointFailureIs(err, failure.CheckpointRestoreRequestConflict) {
			t.Fatalf("conflict err=%v", err)
		}
		if !reflect.DeepEqual(calls, []string{"load_restore"}) {
			t.Fatalf("conflict performed extra work calls=%v", calls)
		}
	})

	t.Run("expired checkpoint", func(t *testing.T) {
		request := restoreCoreRequest([]string{"a.txt"})
		_, checkpoint := createCompletedReservation(t, createTestRequest())
		checkpoint.RetentionState = core.RetentionExpired
		calls := []string{}
		repo := &restoreTestRepository{createTestRepository: &createTestRepository{checkpoint: &checkpoint}, calls: &calls}
		workspace := &restoreTestWorkspace{calls: &calls, context: createWorkspaceContext("/repo", testGeneration)}
		provider := &restoreTestProvider{createTestProvider: &createTestProvider{identity: checkpoint.Provider}, calls: &calls}
		svc := New(repo, workspace, provider)

		_, err := svc.Restore(context.Background(), request)
		if !checkpointFailureIs(err, failure.CheckpointExpired) {
			t.Fatalf("expired err=%v", err)
		}
		if !reflect.DeepEqual(calls, []string{"load_restore", "load_checkpoint"}) || len(provider.restores) != 0 {
			t.Fatalf("expired performed mutation work calls=%v restores=%d", calls, len(provider.restores))
		}
	})
}

func restoreCoreRequest(paths []string) core.RestoreRequest {
	return core.RestoreRequest{RestoreID: "restore-app-1", CheckpointID: testCheckpointID, Paths: append([]string(nil), paths...)}
}

func restoreReservationFor(t *testing.T, request core.RestoreRequest) RestoreReservation {
	t.Helper()
	normalized, err := request.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	fp, err := normalized.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return RestoreReservation{
		SchemaVersion: ReservationSchemaVersion, RestoreID: normalized.RestoreID, RequestFingerprint: fp,
		CheckpointID: normalized.CheckpointID, WorkspaceID: testWorkspaceID, Paths: normalized.Paths,
		StartedAt: time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC),
	}
}

func cloneRestoreResult(in *core.RestoreResult) *core.RestoreResult {
	if in == nil {
		return nil
	}
	out := *in
	out.Paths = append([]core.RestorePathResult(nil), in.Paths...)
	return &out
}

var _ = errors.Is

func TestCheckpointRestoreConflictAndNoopOnlyDoNotInvalidateWorkspace(t *testing.T) {
	request := restoreCoreRequest([]string{"a.txt", "b.txt"})
	_, checkpoint := createCompletedReservation(t, createTestRequest())
	repo := &restoreTestRepository{createTestRepository: &createTestRepository{checkpoint: &checkpoint}}
	workspace := &restoreTestWorkspace{context: createWorkspaceContext("/repo", testGeneration)}
	provider := &restoreTestProvider{
		createTestProvider: &createTestProvider{identity: checkpoint.Provider},
		result: ProviderRestoreResult{Paths: []core.RestorePathResult{
			{Path: "a.txt", Outcome: core.RestoreNoop},
			{Path: "b.txt", Outcome: core.RestoreConflict, Reason: "current_changed"},
		}},
	}
	svc := New(repo, workspace, provider)

	got, err := svc.Restore(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || workspace.invalidated != 0 {
		t.Fatalf("conflict/noop restore=%#v invalidated=%d", got, workspace.invalidated)
	}
}
