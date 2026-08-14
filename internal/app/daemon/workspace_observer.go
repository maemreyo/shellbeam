package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceObserver interface {
	Observe(context.Context, string) workspace.FastSnapshot
}

type FreshWorkspaceObserver interface {
	ObserveFresh(context.Context, string) workspace.FastSnapshot
}

type workspaceObservation struct {
	pre *workspace.FastSnapshot
}

func NewServiceWithWorkspaceObserver(store Store, owner ProcessOwner, observer WorkspaceObserver, options Options) *Service {
	service := NewService(store, owner, options)
	service.observer = observer
	return service
}

func (s *Service) captureWorkspace(ctx context.Context, cwd string) workspaceObservation {
	if s.observer == nil {
		return workspaceObservation{}
	}
	snapshot := s.observer.Observe(ctx, cwd)
	return workspaceObservation{pre: &snapshot}
}

func (s *Service) attachWorkspaceProvenance(rec *receipt.Receipt, observation workspaceObservation, cwd string) {
	if rec == nil || rec.SchemaVersion < 2 || observation.pre == nil || s.observer == nil {
		return
	}
	post := s.observer.Observe(context.Background(), cwd)
	if observer, ok := s.observer.(FreshWorkspaceObserver); ok {
		post = observer.ObserveFresh(context.Background(), cwd)
	}
	rec.WorkspaceProvenance = receipt.NewWorkspaceProvenance(*observation.pre, post)
}
