package checkpoint

import (
	"context"
	"reflect"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

type inspectRetentionRepository struct {
	*createTestRepository
	checkpoints []core.Checkpoint
	marks       []retentionMark
}

type retentionMark struct {
	checkpointID string
	state        core.RetentionState
}

func (r *inspectRetentionRepository) ListCheckpointMetadata(context.Context) ([]core.Checkpoint, error) {
	return append([]core.Checkpoint(nil), r.checkpoints...), nil
}

func (r *inspectRetentionRepository) LoadCheckpoint(_ context.Context, checkpointID string) (core.Checkpoint, error) {
	for _, checkpoint := range r.checkpoints {
		if checkpoint.CheckpointID == checkpointID {
			return checkpoint, nil
		}
	}
	return r.createTestRepository.LoadCheckpoint(context.Background(), checkpointID)
}

func (r *inspectRetentionRepository) MarkCheckpointRetention(_ context.Context, checkpointID string, state core.RetentionState) (core.Checkpoint, error) {
	for i := range r.checkpoints {
		if r.checkpoints[i].CheckpointID == checkpointID {
			r.checkpoints[i].RetentionState = state
			r.marks = append(r.marks, retentionMark{checkpointID: checkpointID, state: state})
			return r.checkpoints[i], nil
		}
	}
	return r.createTestRepository.MarkCheckpointRetention(context.Background(), checkpointID, state)
}

type inspectRetentionProvider struct {
	*createTestProvider
	inspectStatus ProviderCheckpointStatus
	inspectCalls  []string
	sweepResult   SweepResult
	sweepRequests []SweepRequest
}

func (p *inspectRetentionProvider) Inspect(_ context.Context, checkpointID string) (ProviderCheckpointStatus, error) {
	p.inspectCalls = append(p.inspectCalls, checkpointID)
	return p.inspectStatus, nil
}

func (p *inspectRetentionProvider) Sweep(_ context.Context, request SweepRequest) (SweepResult, error) {
	p.sweepRequests = append(p.sweepRequests, request)
	return p.sweepResult, nil
}

func TestCheckpointInspectReturnsPublicMetadataAndProviderAvailabilityWithoutPrivateState(t *testing.T) {
	_, checkpoint := createCompletedReservation(t, createTestRequest())
	checkpoint.RetentionState = core.RetentionAvailable
	repo := &inspectRetentionRepository{
		createTestRepository: &createTestRepository{},
		checkpoints:          []core.Checkpoint{checkpoint},
	}
	provider := &inspectRetentionProvider{
		createTestProvider: &createTestProvider{identity: checkpoint.Provider},
		inspectStatus: ProviderCheckpointStatus{
			CheckpointID:   checkpoint.CheckpointID,
			RetentionState: core.RetentionAvailable,
			Available:      true,
		},
	}
	svc := New(repo, nil, provider)

	got, err := svc.Inspect(context.Background(), checkpoint.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Checkpoint, checkpoint) {
		t.Fatalf("checkpoint=%#v want=%#v", got.Checkpoint, checkpoint)
	}
	if !reflect.DeepEqual(got.Provider, provider.inspectStatus) {
		t.Fatalf("provider status=%#v want=%#v", got.Provider, provider.inspectStatus)
	}
	if !reflect.DeepEqual(provider.inspectCalls, []string{checkpoint.CheckpointID}) {
		t.Fatalf("inspect calls=%v", provider.inspectCalls)
	}
}

func TestCheckpointInspectKeepsExpiredCheckpointInspectable(t *testing.T) {
	_, checkpoint := createCompletedReservation(t, createTestRequest())
	checkpoint.RetentionState = core.RetentionExpired
	repo := &inspectRetentionRepository{
		createTestRepository: &createTestRepository{},
		checkpoints:          []core.Checkpoint{checkpoint},
	}
	provider := &inspectRetentionProvider{
		createTestProvider: &createTestProvider{identity: checkpoint.Provider},
		inspectStatus: ProviderCheckpointStatus{
			CheckpointID:   checkpoint.CheckpointID,
			RetentionState: core.RetentionExpired,
			Available:      false,
		},
	}
	svc := New(repo, nil, provider)

	got, err := svc.Inspect(context.Background(), checkpoint.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Checkpoint.RetentionState != core.RetentionExpired || got.Provider.RetentionState != core.RetentionExpired || got.Provider.Available {
		t.Fatalf("expired inspection=%#v", got)
	}
}

func TestSweepRetentionUsesDefaultBudgetsAndMarksOnlyProviderExpiredCheckpoints(t *testing.T) {
	_, first := createCompletedReservation(t, createTestRequest())
	second := first
	second.CheckpointID = "chk_01K00000000000000000000061"
	repo := &inspectRetentionRepository{
		createTestRepository: &createTestRepository{},
		checkpoints:          []core.Checkpoint{first, second},
	}
	provider := &inspectRetentionProvider{
		createTestProvider: &createTestProvider{identity: first.Provider},
		sweepResult: SweepResult{
			ExpiredCheckpointIDs: []string{first.CheckpointID},
			FreedBytes:           123,
		},
	}
	svc := New(repo, nil, provider)
	now := time.Date(2026, 8, 16, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	got, err := svc.SweepRetention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, provider.sweepResult) {
		t.Fatalf("sweep=%#v want=%#v", got, provider.sweepResult)
	}
	wantRequest := SweepRequest{
		Now:            now,
		MaxCheckpoints: core.MaxRetainedCheckpoints,
		MaxBytes:       core.MaxPrivateProviderBytes,
		MaxAge:         core.MaxRetentionAge,
	}
	if !reflect.DeepEqual(provider.sweepRequests, []SweepRequest{wantRequest}) {
		t.Fatalf("sweep requests=%#v want=%#v", provider.sweepRequests, wantRequest)
	}
	if !reflect.DeepEqual(repo.marks, []retentionMark{{checkpointID: first.CheckpointID, state: core.RetentionExpired}}) {
		t.Fatalf("retention marks=%#v", repo.marks)
	}
	if repo.checkpoints[1].RetentionState != core.RetentionAvailable {
		t.Fatalf("non-expired checkpoint changed=%#v", repo.checkpoints[1])
	}
}
