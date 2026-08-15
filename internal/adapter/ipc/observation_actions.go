package ipc

import (
	"context"

	appenv "github.com/maemreyo/shellbeam/internal/app/environment"
	appprocess "github.com/maemreyo/shellbeam/internal/app/process"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type EnvironmentActions interface {
	InspectEnvironment(context.Context, appenv.InspectRequest) (environment.Snapshot, error)
}

type ProcessInspectionActions interface {
	InspectProcess(context.Context, appprocess.InspectRequest) (processcore.Observation, error)
}

type EnvironmentRequest = appenv.InspectRequest
type ProcessRequest = appprocess.InspectRequest
type EnvironmentResponse = environment.Snapshot
type ProcessResponse = processcore.Observation
