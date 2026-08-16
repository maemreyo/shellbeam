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

type createTestRepository struct {
	calls         *[]string
	reservation   *CreateReservation
	checkpoint    *core.Checkpoint
	pathResults   map[int]core.RestorePathResult
	finalRestore  *core.RestoreResult
	reserveCalls  int
	completeCalls int
}

func (r *createTestRepository) call(name string) {
	if r.calls != nil {
		*r.calls = append(*r.calls, name)
	}
}
func (r *createTestRepository) ReserveCheckpointCreate(_ context.Context, in CreateReservation) (CreateReservation, *core.Checkpoint, bool, error) {
	r.call("reserve")
	r.reserveCalls++
	if r.reservation != nil {
		if r.reservation.RequestFingerprint != in.RequestFingerprint {
			return CreateReservation{}, nil, false, failure.New(failure.CheckpointCreateConflict, map[string]string{"checkpoint_create_id": in.CreateID}, nil)
		}
		copy := *r.reservation
		return copy, cloneCheckpoint(r.checkpoint), false, nil
	}
	copy := in
	r.reservation = &copy
	return copy, nil, true, nil
}
func (r *createTestRepository) BindCheckpointSource(_ context.Context, createID, generation string) (CreateReservation, error) {
	r.call("bind_source")
	if r.reservation == nil || r.reservation.CreateID != createID {
		return CreateReservation{}, errors.New("missing reservation")
	}
	if r.reservation.SourceGeneration != "" && r.reservation.SourceGeneration != generation {
		return CreateReservation{}, failure.New(failure.CheckpointCreateConflict, map[string]string{"checkpoint_create_id": createID}, nil)
	}
	r.reservation.SourceGeneration = generation
	return *r.reservation, nil
}
func (r *createTestRepository) CompleteCheckpointCreate(_ context.Context, createID string, in core.Checkpoint) (core.Checkpoint, error) {
	r.call("complete")
	r.completeCalls++
	if r.reservation == nil || r.reservation.CreateID != createID {
		return core.Checkpoint{}, errors.New("missing reservation")
	}
	if r.checkpoint != nil {
		if !reflect.DeepEqual(*r.checkpoint, in) {
			return core.Checkpoint{}, failure.New(failure.CheckpointCreateConflict, map[string]string{"checkpoint_create_id": createID}, nil)
		}
		return *r.checkpoint, nil
	}
	copy := in
	r.checkpoint = &copy
	return copy, nil
}
func (r *createTestRepository) FindCheckpointByCreateID(_ context.Context, createID string) (CreateReservation, *core.Checkpoint, bool, error) {
	r.call("find")
	if r.reservation == nil || r.reservation.CreateID != createID {
		return CreateReservation{}, nil, false, nil
	}
	return *r.reservation, cloneCheckpoint(r.checkpoint), true, nil
}
func (r *createTestRepository) LoadCheckpoint(_ context.Context, id string) (core.Checkpoint, error) {
	if r.checkpoint != nil && r.checkpoint.CheckpointID == id {
		return *r.checkpoint, nil
	}
	return core.Checkpoint{}, failure.New(failure.CheckpointNotFound, map[string]string{"checkpoint_id": id}, nil)
}
func (r *createTestRepository) ListCheckpointMetadata(context.Context) ([]core.Checkpoint, error) {
	return nil, nil
}
func (r *createTestRepository) MarkCheckpointRetention(context.Context, string, core.RetentionState) (core.Checkpoint, error) {
	return core.Checkpoint{}, errors.New("unused")
}
func (r *createTestRepository) ReserveCheckpointRestore(context.Context, RestoreReservation) (RestoreReservation, *core.RestoreResult, bool, error) {
	return RestoreReservation{}, nil, false, errors.New("unused")
}
func (r *createTestRepository) RecordCheckpointRestorePath(_ context.Context, _ string, ordinal int, result core.RestorePathResult) error {
	if r.pathResults == nil {
		r.pathResults = map[int]core.RestorePathResult{}
	}
	r.pathResults[ordinal] = result
	return nil
}
func (r *createTestRepository) CompleteCheckpointRestore(context.Context, string, core.RestoreResult) (core.RestoreResult, error) {
	return core.RestoreResult{}, errors.New("unused")
}
func (r *createTestRepository) LoadCheckpointRestore(context.Context, string) (RestoreReservation, []core.RestorePathResult, *core.RestoreResult, error) {
	return RestoreReservation{}, nil, r.finalRestore, errors.New("unused")
}

func cloneCheckpoint(in *core.Checkpoint) *core.Checkpoint {
	if in == nil {
		return nil
	}
	out := *in
	out.OpaqueEntryRefs = append([]string(nil), in.OpaqueEntryRefs...)
	return &out
}

type createTestWorkspace struct {
	calls        *[]string
	contexts     []WorkspaceContext
	resolveCalls int
}

func (w *createTestWorkspace) ResolveFresh(context.Context, string) (WorkspaceContext, error) {
	if w.calls != nil {
		*w.calls = append(*w.calls, "workspace.resolve")
	}
	w.resolveCalls++
	if len(w.contexts) == 0 {
		return WorkspaceContext{}, errors.New("workspace unavailable")
	}
	idx := w.resolveCalls - 1
	if idx >= len(w.contexts) {
		idx = len(w.contexts) - 1
	}
	return w.contexts[idx], nil
}
func (w *createTestWorkspace) InvalidateAfterMutation(context.Context, string) error { return nil }

type createTestProvider struct {
	calls    *[]string
	identity core.ProviderIdentity
	captures []CaptureRequest
	results  []CaptureResult
	errs     []error
}

func (p *createTestProvider) Identity() core.ProviderIdentity {
	if p.calls != nil {
		*p.calls = append(*p.calls, "provider.identity")
	}
	return p.identity
}
func (p *createTestProvider) ConflictDetection() core.ConflictDetection {
	return core.ConflictDetection{RegularFile: core.ConflictBestEffort, Symlink: core.ConflictBestEffort, AbsentToFile: core.ConflictBestEffort, DirectoryTree: core.ConflictUnsupported}
}
func (p *createTestProvider) Capture(_ context.Context, req CaptureRequest) (CaptureResult, error) {
	if p.calls != nil {
		*p.calls = append(*p.calls, "provider.capture")
	}
	p.captures = append(p.captures, req)
	idx := len(p.captures) - 1
	if idx < len(p.errs) && p.errs[idx] != nil {
		return CaptureResult{}, p.errs[idx]
	}
	if idx < len(p.results) {
		return p.results[idx], nil
	}
	return CaptureResult{}, errors.New("capture result missing")
}
func (p *createTestProvider) Restore(context.Context, ProviderRestoreRequest) (ProviderRestoreResult, error) {
	return ProviderRestoreResult{}, errors.New("unused")
}
func (p *createTestProvider) Inspect(context.Context, string) (ProviderCheckpointStatus, error) {
	return ProviderCheckpointStatus{}, errors.New("unused")
}
func (p *createTestProvider) Sweep(context.Context, SweepRequest) (SweepResult, error) {
	return SweepResult{}, errors.New("unused")
}

func createTestRequest() core.CreateRequest {
	return core.CreateRequest{CreateID: "cp-create-1", WorkspaceID: testWorkspaceID, ActivityID: "PI-756", Paths: []string{"internal/runtime/file.go", "tests/runtime/**"}}
}
func createWorkspaceContext(root, generation string) WorkspaceContext {
	return WorkspaceContext{WorkspaceID: testWorkspaceID, RepositoryID: "repo_01K00000000000000000000000", Root: root, SourceGeneration: generation}
}
func createCaptureResult() CaptureResult {
	return CaptureResult{CapturedPathCount: 1, TotalBytes: 7, CaptureQuality: core.CaptureComplete, OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"}}
}
func createCompletedReservation(t *testing.T, request core.CreateRequest) (CreateReservation, core.Checkpoint) {
	t.Helper()
	fp, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	reservation := CreateReservation{SchemaVersion: ReservationSchemaVersion, CreateID: request.CreateID, RequestFingerprint: fp, CheckpointID: testCheckpointID, Provider: core.ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: request.WorkspaceID, ActivityID: request.ActivityID, Paths: []string{"internal/runtime/file.go", "tests/runtime/**"}, SourceGeneration: testGeneration, CreatedAt: created}
	checkpoint := core.Checkpoint{SchemaVersion: core.SchemaVersion, CheckpointID: reservation.CheckpointID, CreateID: reservation.CreateID, Provider: reservation.Provider, WorkspaceID: reservation.WorkspaceID, ActivityID: reservation.ActivityID, SourceGeneration: reservation.SourceGeneration, CreatedAt: created, CapturedPathCount: 1, TotalBytes: 7, CaptureQuality: core.CaptureComplete, RetentionState: core.RetentionAvailable, OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"}}
	return reservation, checkpoint
}
func checkpointFailureIs(err error, code failure.Code) bool {
	var typed *failure.Failure
	return errors.As(err, &typed) && typed.Code == code
}
func validCheckpointID(id string) bool { return strings.HasPrefix(id, "chk_") && len(id) == 30 }
