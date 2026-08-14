package activity

import (
	"context"
	"fmt"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Service struct {
	registry   Registry
	samples    WorkspaceSampleSource
	maxHistory int
	mu         sync.Mutex
	locks      map[core.ID]*sync.Mutex
}

func New(registry Registry, samples WorkspaceSampleSource, maxHistory int) *Service {
	if maxHistory <= 0 {
		maxHistory = core.MaxOperationHistory
	}
	return &Service{registry: registry, samples: samples, maxHistory: maxHistory, locks: map[core.ID]*sync.Mutex{}}
}

func (s *Service) Inspect(ctx context.Context, rawID string) (core.Activity, error) {
	id, err := core.ParseID(rawID)
	if err != nil {
		return core.Activity{}, failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
	}
	record, found, err := s.registry.LoadActivity(ctx, id)
	if err != nil {
		return core.Activity{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	if !found {
		return core.Activity{}, failure.New(failure.ActivityNotFound, map[string]string{"activity_id": rawID}, nil)
	}
	if err := record.Validate(s.maxHistory); err != nil {
		return core.Activity{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	return record, nil
}

func (s *Service) Admit(ctx context.Context, admission core.Admission) (core.Activity, error) {
	id, err := core.ParseID(string(admission.ActivityID))
	if err != nil {
		return core.Activity{}, err
	}
	if admission.OperationID == "" || admission.SessionID == "" {
		return core.Activity{}, fmt.Errorf("activity admission identity missing")
	}
	if admission.ObservedAt.IsZero() {
		admission.ObservedAt = time.Now().UTC()
	}
	unlock := s.lock(id)
	defer unlock()

	record, found, err := s.registry.LoadActivity(ctx, id)
	if err != nil {
		return core.Activity{}, err
	}
	if !found {
		record = core.New(id, admission.ObservedAt)
	}
	if admission.WorkspaceID != "" {
		if _, exists := record.BaselineFor(admission.WorkspaceID); !exists && s.samples != nil {
			sample := s.samples.Sample(ctx, admission.WorkspaceID, workspace.DeltaLimits{})
			observation := workspaceObservationFromSample(sample)
			if observation.WorkspaceID == "" || observation.WorkspaceID != admission.WorkspaceID {
				observation.WorkspaceID = admission.WorkspaceID
				observation.Quality = workspace.QualityUnavailable
				observation.Completeness = workspace.SelectionUnavailable
				observation.Paths = nil
				observation.PathsTruncated = true
			}
			record.AddBaseline(core.BaselineFrom(observation))
		}
	}
	record.ObserveOperation(core.OperationRef{OperationID: admission.OperationID, SessionID: admission.SessionID, WorkspaceID: admission.WorkspaceID, ObservedAt: admission.ObservedAt}, s.maxHistory)
	if err := record.Validate(s.maxHistory); err != nil {
		return core.Activity{}, err
	}
	if err := s.registry.SaveActivity(ctx, record); err != nil {
		return core.Activity{}, err
	}
	return record, nil
}

func (s *Service) CompareWorkspace(ctx context.Context, rawID string, sample workspace.DeltaSample) (core.Comparison, error) {
	record, err := s.Inspect(ctx, rawID)
	if err != nil {
		return core.Comparison{}, err
	}
	baseline, found := record.BaselineFor(sample.WorkspaceID)
	if !found {
		return core.Comparison{BaselineDiverged: true, DivergenceReason: "evidence_unavailable"}, nil
	}
	return core.Compare(baseline, workspaceObservationFromSample(sample)), nil
}

func (s *Service) lock(id core.ID) func() {
	s.mu.Lock()
	lock := s.locks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[id] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
