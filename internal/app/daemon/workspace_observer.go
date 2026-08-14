package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceObserver interface {
	Bind(context.Context, string) workspace.Binding
	ObserveCached(context.Context, string) workspace.FastSnapshot
}

type workspaceObservation struct {
	binding workspace.Binding
	pre     receipt.WorkspaceObservationRef
	context *workspace.FastSnapshot
}

func NewServiceWithWorkspaceObserver(store Store, owner ProcessOwner, observer WorkspaceObserver, options Options) *Service {
	service := NewService(store, owner, options)
	service.observer = observer
	return service
}

func NewServiceWithExecutionContextAndCoherence(store Store, owner ProcessOwner, resolver WorkspaceResolver, observer WorkspaceObserver, tracker ActivityTracker, coherence WorkspaceCoherence, options Options) *Service {
	service := NewServiceWithExecutionContext(store, owner, resolver, observer, tracker, options)
	service.coherence = coherence
	return service
}

func (s *Service) captureWorkspace(ctx context.Context, cwd string) workspaceObservation {
	observation := workspaceObservation{pre: receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled}}
	if s.observer == nil {
		return observation
	}
	observation.binding = s.observer.Bind(ctx, cwd)
	cached := s.observer.ObserveCached(ctx, cwd)
	if cached.Quality == workspace.QualityUnavailable || cached.Generation == "" || cached.ObservedAt.IsZero() {
		observation.pre.DiagnosticCode = cached.DiagnosticCode
		return observation
	}
	observation.pre = receipt.WorkspaceObservationRef{
		Kind:           receipt.WorkspaceCached,
		Generation:     cached.Generation,
		Quality:        cached.Quality,
		ObservedAt:     cached.ObservedAt,
		DiagnosticCode: cached.DiagnosticCode,
	}
	copy := cached
	observation.context = &copy
	return observation
}

func (s *Service) attachWorkspaceProvenance(rec *receipt.Receipt, observation workspaceObservation) {
	if rec == nil || rec.SchemaVersion < 2 {
		return
	}
	binding := receipt.WorkspaceBinding{RepositoryID: observation.binding.RepositoryID, WorkspaceID: observation.binding.WorkspaceID}
	post := receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceUnreconciled, ObservationInvalidated: true}
	rec.WorkspaceProvenance = receipt.NewWorkspaceProvenanceV2(binding, observation.pre, post, false)
}
