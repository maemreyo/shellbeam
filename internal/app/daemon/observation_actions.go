package daemon

import (
	"context"

	appenv "github.com/maemreyo/shellbeam/internal/app/environment"
	appprocess "github.com/maemreyo/shellbeam/internal/app/process"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type EnvironmentInspector interface {
	Inspect(context.Context, appenv.InspectRequest) (environment.Snapshot, error)
}

type ProcessInspector interface {
	Inspect(context.Context, appprocess.Request) (processcore.Observation, error)
}

func (s *Service) SetObservationInspectors(environmentInspector EnvironmentInspector, processInspector ProcessInspector) {
	if s == nil {
		return
	}
	s.environmentInspector = environmentInspector
	s.processInspector = processInspector
}

func (s *Service) InspectEnvironment(ctx context.Context, request appenv.InspectRequest) (environment.Snapshot, error) {
	if s == nil || s.environmentInspector == nil {
		return environment.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "observer_not_configured"}, nil)
	}
	return s.environmentInspector.Inspect(ctx, request)
}

func (s *Service) InspectProcess(ctx context.Context, request appprocess.Request) (processcore.Observation, error) {
	if s == nil || s.processInspector == nil {
		return processcore.Observation{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"reason": "observer_not_configured"}, nil)
	}
	return s.processInspector.Inspect(ctx, request)
}
