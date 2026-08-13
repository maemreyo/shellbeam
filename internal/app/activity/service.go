package activity

import (
	"context"
	"fmt"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/activity"
)

type Service struct {
	registry   Registry
	baseline   BaselineSource
	maxHistory int
	mu         sync.Mutex
	locks      map[core.ID]*sync.Mutex
}

func New(registry Registry, baseline BaselineSource, maxHistory int) *Service {
	if maxHistory <= 0 {
		maxHistory = core.MaxOperationHistory
	}
	return &Service{registry: registry, baseline: baseline, maxHistory: maxHistory, locks: map[core.ID]*sync.Mutex{}}
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
		if _, exists := record.BaselineFor(admission.WorkspaceID); !exists && s.baseline != nil {
			observation := s.baseline.CaptureBaseline(ctx, admission.WorkspaceID, admission.CWD)
			if observation.WorkspaceID == "" {
				observation.WorkspaceID = admission.WorkspaceID
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
