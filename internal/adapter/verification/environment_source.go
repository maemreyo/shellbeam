package verification

import (
	"context"
	"fmt"

	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

type currentEnvironmentInspector interface {
	Inspect(context.Context, environmentapp.InspectRequest) (environment.Snapshot, error)
}

type EnvironmentSource struct {
	inspector currentEnvironmentInspector
}

func NewEnvironmentSource(inspector currentEnvironmentInspector) *EnvironmentSource {
	return &EnvironmentSource{inspector: inspector}
}

func (s *EnvironmentSource) CurrentBinding(ctx context.Context, workspaceID string) (environment.Binding, bool, error) {
	if s == nil || s.inspector == nil {
		return environment.Binding{}, false, fmt.Errorf("environment inspector unavailable")
	}
	snapshot, err := s.inspector.Inspect(ctx, environmentapp.InspectRequest{WorkspaceID: workspaceID, Freshness: environment.FreshnessRefresh})
	if err != nil {
		return environment.Binding{}, false, err
	}
	if err := snapshot.Validate(); err != nil {
		return environment.Binding{}, false, fmt.Errorf("invalid current environment snapshot: %w", err)
	}
	binding := snapshot.Binding()
	if err := binding.Validate(); err != nil {
		return environment.Binding{}, false, fmt.Errorf("invalid current environment binding: %w", err)
	}
	return binding, true, nil
}
