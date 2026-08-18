//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"net/http"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
)

func (s *Server) handleV2(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRequestV2(r.Body)
	if err != nil {
		action := req.Action
		if action == "" {
			action = "unknown"
		}
		writeResponseV2(w, ResponseV2{IPVersion: ipcV2, Kind: "response", RequestID: req.RequestID, Action: action, OK: false, Error: errorEnvelope(err)})
		return
	}
	if err := s.awaitReady(r.Context()); err != nil {
		writeResponseV2(w, ResponseV2{IPVersion: ipcV2, Kind: "response", RequestID: req.RequestID, Action: req.Action, OK: false, Error: errorEnvelope(err)})
		return
	}
	resp := ResponseV2{IPVersion: ipcV2, Kind: "response", RequestID: req.RequestID, Action: req.Action}
	err = s.dispatchV2(r.Context(), req, &resp)
	resp.OK = err == nil
	if err != nil {
		clearResponseV2Payload(&resp)
		resp.Error = errorEnvelope(err)
	}
	writeResponseV2(w, resp)
}

func (s *Server) dispatchV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	var err error
	switch req.Action {
	case "start":
		view, callErr := s.actions.Start(ctx, app.StartRequest{ProtocolVersion: 2, OperationID: req.OperationID, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, WorkspaceHint: req.WorkspaceHint, StructuredAdapter: req.StructuredAdapter, ProjectCommandID: req.ProjectCommandID, Params: cloneStringMapV2(req.Params), Command: req.Command, Argv: append([]string(nil), req.Argv...), Intent: req.Intent, Evidence: req.Evidence, CWD: req.CWD, TTY: req.TTY, Persistent: req.Persistent, SessionName: req.SessionName, TimeoutMS: req.TimeoutMS, StdinMode: req.StdinMode, TimeoutMode: req.TimeoutMode, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes, TraceMode: req.TraceMode, ResourceLimits: req.ResourceLimits.Clone(), Hermetic: req.Hermetic.Clone()})
		err = callErr
		if err == nil {
			result, resultErr := view.StructuredResult()
			err = resultErr
			if resultErr == nil {
				resp.Result = &result
			}
		}
		if err == nil {
			_ = s.decorateStartInputTraceV2(ctx, req, resp)
		}
	case "checkpoint_create", "checkpoint_restore", "checkpoint_inspect":
		err = s.checkpointV2(ctx, req, resp)
	case "poll":
		view, callErr := s.actions.Poll(ctx, app.PollRequest{SessionID: req.SessionID, Cursor: req.Cursor, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
		err = callErr
		if err == nil {
			result, resultErr := view.StructuredResult()
			err = resultErr
			if resultErr == nil {
				resp.Result = &result
			}
		}
	case "read_output":
		actions, ok := s.actions.(OutputViewActions)
		if !ok {
			err = failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
			break
		}
		result, callErr := actions.ReadOutputView(ctx, outputview.Request{SessionID: req.SessionID, Selector: *req.Selector, Continuation: req.Continuation})
		err = callErr
		if err == nil {
			resp.OutputView = &result
		}
	case "write":
		view, callErr := s.actions.Write(ctx, app.WriteRequest{SessionID: req.SessionID, InputOffset: req.InputOffset, Chars: req.Chars, EOF: req.EOF})
		err = callErr
		resp.View = &view
	case "kill":
		view, callErr := s.actions.Kill(ctx, app.KillRequest{SessionID: req.SessionID, KillID: req.KillID, Signal: req.Signal})
		err = callErr
		resp.View = &view
	case "repro.create":
		actions, ok := s.actions.(ReproActions)
		if !ok {
			err = failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
			break
		}
		policy := reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}
		if req.CapturePolicy != nil {
			policy = *req.CapturePolicy
		}
		capsule, callErr := actions.CreateRepro(ctx, reprocore.CreateRequest{CreateID: req.ReproCreateID, OperationID: req.OperationID, Policy: policy})
		err = callErr
		if err == nil {
			resp.Capsule = &capsule
		}
	case "capabilities.negotiate", "read_media":
		err = dispatchMediaV2(ctx, req, resp, s.actions)
	case "inspect.server", "inspect.workspace", "inspect.activity", "inspect.sessions", "inspect.project", "inspect.readiness", "inspect.events", "inspect.structured", "inspect.telemetry", "inspect.trace", "inspect.evidence", "inspect.environment", "inspect.process", "inspect.repro", "inspect.code", "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		err = s.inspectV2(ctx, req, resp)
	}
	return err
}

func (s *Server) inspectV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	switch req.Action {
	case "inspect.server":
		info, err := s.actions.InspectServer(ctx)
		resp.Server = &info.Capabilities
		return err
	case "inspect.workspace":
		actions, ok := s.actions.(WorkspaceActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		record, err := actions.InspectWorkspace(ctx, req.WorkspaceID)
		resp.Workspace = &record
		return err
	case "inspect.activity":
		return s.inspectActivityV2(ctx, req, resp)
	case "inspect.sessions":
		return s.inspectSessionsV2(ctx, req, resp)
	case "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		return s.mutationScopeV2(ctx, req, resp)
	case "inspect.project", "inspect.readiness":
		return s.inspectProjectV2(ctx, req, resp)
	case "inspect.structured":
		actions, ok := s.actions.(StructuredActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		request := structuredapp.InspectRequest{OperationID: req.OperationID, Filter: structuredapp.RecordFilter{RecordKind: req.RecordKind, Severity: req.Severity, Path: req.Path, TestStatus: req.TestStatus}, Continuation: req.Continuation, MaxRecords: req.MaxRecords}
		result, err := actions.InspectStructured(ctx, request)
		resp.Structured = &result
		return err
	case "inspect.trace":
		return s.inspectInputTraceV2(ctx, req, resp)
	case "inspect.environment", "inspect.process":
		return s.inspectEnvironmentProcessV2(ctx, req, resp)
	case "inspect.evidence":
		actions, ok := s.actions.(EvidenceActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		request := evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{EvidenceID: req.EvidenceID, OperationID: req.OperationID, WorkspaceID: req.WorkspaceID, ProjectCommandID: req.ProjectCommandID, ActivityID: req.ActivityID, VerificationKind: req.VerificationKind, Result: req.EvidenceResult, RevalidateArtifacts: req.RevalidateArtifacts}, Continuation: req.Continuation, MaxRecords: req.MaxRecords}
		result, err := actions.InspectEvidence(ctx, request)
		resp.Evidence = &result
		return err
	case "inspect.telemetry":
		actions, ok := s.actions.(TelemetryActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		result, err := actions.InspectTelemetry(ctx, telemetryapp.InspectRequest{OperationID: req.OperationID, MaxSamples: req.MaxSamples})
		resp.Telemetry = &result
		return err
	case "inspect.repro":
		actions, ok := s.actions.(ReproActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		result, err := actions.InspectRepro(ctx, req.ReproID)
		resp.Repro = &result
		return err
	case "inspect.code":
		actions, ok := s.actions.(CodeActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		result, err := actions.InspectCode(ctx, req.WorkspaceID, req.ActivityID, *req.CodeQuery)
		resp.Code = &result
		return err
	case "inspect.events":
		actions, ok := s.actions.(EventActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		result, err := actions.InspectEvents(ctx, observationapp.InspectRequest{Target: *req.Target, AfterEventCursor: req.AfterEventCursor, MaxEvents: req.MaxEvents})
		resp.Events = &result
		return err
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
}

func (s *Server) inspectEnvironmentProcessV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	if req.Action == "inspect.environment" {
		actions, ok := s.actions.(EnvironmentActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		request := EnvironmentRequest{WorkspaceID: req.WorkspaceID, Freshness: req.Freshness}
		if req.Execution != nil {
			execution := *req.Execution
			request.Execution = &execution
		}
		result, err := actions.InspectEnvironment(ctx, request)
		resp.Environment = &result
		return err
	}
	actions, ok := s.actions.(ProcessInspectionActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	request := ProcessRequest{Target: *req.ProcessTarget, IncludePorts: req.IncludePorts}
	result, err := actions.InspectProcess(ctx, request)
	resp.Process = &result
	return err
}

func (s *Server) inspectProjectV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	switch req.Action {
	case "inspect.project":
		actions, ok := s.actions.(ProjectActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		inspection, err := actions.InspectProject(ctx, req.WorkspaceID)
		resp.Project = &inspection
		return err
	case "inspect.readiness":
		actions, ok := s.actions.(ProjectReadinessActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		readiness, err := actions.InspectProjectReadiness(ctx, req.WorkspaceID)
		resp.Readiness = &readiness
		return err
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
}

func writeResponseV2(w http.ResponseWriter, response ResponseV2) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
