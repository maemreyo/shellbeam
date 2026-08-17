package daemon

import (
	"context"
	"errors"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (s *Service) prepareInputTrace(ctx context.Context, req StartRequest, reservation operation.Reservation, spec operation.ExecutionSpec) (operation.Reservation, operation.ExecutionSpec, traceapp.Prepared, error) {
	mode, err := trace.NormalizeMode(req.TraceMode)
	if err != nil {
		return reservation, spec, nil, failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	if mode == trace.ModeOff {
		return reservation, spec, nil, nil
	}
	preparation, err := traceapp.New(s.options.InputTracePreparer).Prepare(ctx, traceapp.PrepareRequest{
		Mode: mode, OperationID: req.OperationID, WorkspaceID: req.WorkspaceID,
		ExecutionMode: spec.Mode, Executable: spec.Executable, CWD: spec.CWD,
	})
	if err != nil {
		if ctx.Err() != nil {
			return reservation, spec, nil, ctx.Err()
		}
		if mode == trace.ModeBestEffort {
			return reservation, spec, nil, nil
		}
		if errors.Is(err, failure.InputTraceProviderUnavailable) {
			return reservation, spec, nil, failure.New(failure.InputTraceRequiredUnavailable, map[string]string{"reason": "provider_unavailable"}, err)
		}
		return reservation, spec, nil, err
	}
	if preparation.Binding == nil || preparation.Handle == nil {
		return reservation, spec, nil, failure.New(failure.InputTraceProviderUnavailable, map[string]string{"reason": "invalid_preparation"}, nil)
	}
	digest, err := preparation.Binding.Digest()
	if err != nil {
		_ = preparation.Handle.Abort()
		return reservation, spec, nil, failure.New(failure.InputTraceProviderUnavailable, map[string]string{"reason": "invalid_binding"}, err)
	}
	executionFingerprint, err := operation.BindInputTraceExecutionFingerprint(reservation.ExecutionFingerprint, mode, digest)
	if err != nil {
		_ = preparation.Handle.Abort()
		return reservation, spec, nil, failure.New(failure.InputTraceProviderUnavailable, map[string]string{"reason": "invalid_execution_binding"}, err)
	}
	binding := *preparation.Binding
	reservation.Trace = &binding
	reservation.ExecutionFingerprint = executionFingerprint
	spec.EnvironmentAdditions = append([]operation.EnvironmentEntry(nil), preparation.EnvironmentAdditions...)
	return reservation, spec, preparation.Handle, nil
}

func abortPreparedTrace(prepared traceapp.Prepared) {
	if prepared != nil {
		_ = prepared.Abort()
	}
}
