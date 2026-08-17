package ipc

import (
	"context"

	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type InputTraceActions interface {
	InspectInputTrace(context.Context, inputtraceapp.InspectRequest) (inputtraceapp.InspectResult, error)
}

func validateInputTraceRequestV2(v RequestV2) error {
	if _, err := operation.ParseID(v.OperationID); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "operation_id"}, err)
	}
	if v.MaxResources < 1 || v.MaxResources > trace.MaxPublicResources {
		return failure.New(failure.InvalidInput, map[string]string{"field": "max_resources"}, nil)
	}
	return nil
}

func (s *Server) inspectInputTraceV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(InputTraceActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	result, err := actions.InspectInputTrace(ctx, inputtraceapp.InspectRequest{OperationID: req.OperationID, MaxResources: req.MaxResources})
	if err == nil {
		resp.InputTrace = &result
	}
	return err
}

func (s *Server) decorateStartInputTraceV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	mode, err := trace.NormalizeMode(req.TraceMode)
	if err != nil || mode == trace.ModeOff {
		return err
	}
	actions, ok := s.actions.(InputTraceActions)
	if !ok {
		return nil
	}
	result, err := actions.InspectInputTrace(ctx, inputtraceapp.InspectRequest{OperationID: req.OperationID, MaxResources: 1})
	if err != nil {
		return nil
	}
	result.Record = nil
	result.ResourcesReturned = 0
	result.ResourcesAvailable = 0
	resp.InputTrace = &result
	return nil
}
