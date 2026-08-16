//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Actions interface {
	Start(context.Context, app.StartRequest) (app.View, error)
	Poll(context.Context, app.PollRequest) (app.View, error)
	Write(context.Context, app.WriteRequest) (app.View, error)
	Kill(context.Context, app.KillRequest) (app.View, error)
	InspectServer(context.Context) (app.ServerInfo, error)
}

type OutputViewActions interface {
	ReadOutputView(context.Context, outputview.Request) (outputview.Result, error)
}

type SessionInspectActions interface {
	InspectSessions(context.Context, persistent.InspectRequest) (persistent.InspectPage, error)
}

type EventActions interface {
	InspectEvents(context.Context, observationapp.InspectRequest) (observationapp.InspectResult, error)
}

type StructuredActions interface {
	InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error)
}

type EvidenceActions interface {
	InspectEvidence(context.Context, evidenceapp.InspectRequest) (evidenceapp.InspectResult, error)
}

type TelemetryActions interface {
	InspectTelemetry(context.Context, telemetryapp.InspectRequest) (telemetryapp.InspectResult, error)
}

type ReproActions interface {
	CreateRepro(context.Context, reprocore.CreateRequest) (reprocore.Capsule, error)
	InspectRepro(context.Context, string) (reproapp.InspectResult, error)
}

type CodeActions interface {
	InspectCode(context.Context, string, string, codeintel.Query) (codeintel.Result, error)
}

type ProjectActions interface {
	InspectProject(context.Context, string) (project.Inspection, error)
}

type ProjectReadinessActions interface {
	InspectProjectReadiness(context.Context, string) (project.Readiness, error)
}

type WorkspaceActions interface {
	InspectWorkspace(context.Context, string) (workspace.Workspace, error)
}

type ActivityActions interface {
	InspectActivity(context.Context, string) (activity.Activity, error)
}

type Server struct {
	socket     string
	socketInfo os.FileInfo
	listener   net.Listener
	http       *http.Server
	actions    Actions
	ready      chan struct{}
	readyOnce  sync.Once
	closing    chan struct{}
	closeOnce  sync.Once
}

func Listen(runtime string, actions Actions) (*Server, error) {
	return listen(runtime, actions, dialUnixSocket)
}

func ListenPending(runtime string, actions Actions) (*Server, error) {
	return listenWithReadiness(runtime, actions, dialUnixSocket, false)
}

func listen(runtime string, actions Actions, dial socketDialer) (*Server, error) {
	return listenWithReadiness(runtime, actions, dial, true)
}

func listenWithReadiness(runtime string, actions Actions, dial socketDialer, ready bool) (*Server, error) {
	if err := prepareRuntime(runtime); err != nil {
		return nil, err
	}
	lock, err := acquireStartupLock(runtime)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	socket := filepath.Join(runtime, "daemon.sock")
	ln, socketInfo, err := claimSocket(socket, dial)
	if err != nil {
		return nil, err
	}
	auth := &authListener{Listener: ln, uid: uint32(os.Getuid())}
	s := &Server{
		socket: socket, socketInfo: socketInfo, listener: auth, actions: actions,
		ready: make(chan struct{}), closing: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/local-shell", s.handle)
	mux.HandleFunc("POST /v2/local-shell", s.handleV2)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	if ready {
		s.MarkReady()
	}
	return s, nil
}

func (s *Server) SocketPath() string { return s.socket }
func (s *Server) MarkReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

func (s *Server) awaitReady(ctx context.Context) error {
	select {
	case <-s.closing:
		return http.ErrServerClosed
	default:
	}
	select {
	case <-s.ready:
		select {
		case <-s.closing:
			return http.ErrServerClosed
		default:
			return nil
		}
	case <-s.closing:
		return http.ErrServerClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func (s *Server) Close() error {
	s.closeOnce.Do(func() { close(s.closing) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.http.Shutdown(ctx)
	if removeErr := s.removeOwnedSocket(); err == nil && removeErr != nil {
		err = removeErr
	}
	return err
}

func (s *Server) removeOwnedSocket() error {
	lock, err := acquireStartupLock(filepath.Dir(s.socket))
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := os.Lstat(s.socket)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if current.Mode()&os.ModeSocket == 0 || s.socketInfo == nil || !os.SameFile(s.socketInfo, current) {
		return nil
	}
	return os.Remove(s.socket)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRequest(r.Body)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := s.awaitReady(r.Context()); err != nil {
		http.Error(w, "daemon_not_ready", http.StatusServiceUnavailable)
		return
	}
	var view app.View
	switch req.Payload.Action {
	case "start":
		view, err = s.actions.Start(r.Context(), app.StartRequest{OperationID: req.Payload.OperationID, Command: req.Payload.Command, CWD: req.Payload.CWD, TTY: req.Payload.TTY, TimeoutMS: req.Payload.TimeoutMS, YieldMS: req.Payload.YieldMS, MaxOutputBytes: req.Payload.MaxOutputBytes})
	case "poll":
		view, err = s.actions.Poll(r.Context(), app.PollRequest{SessionID: req.Payload.SessionID, Cursor: req.Payload.Cursor, YieldMS: req.Payload.YieldMS, MaxOutputBytes: req.Payload.MaxOutputBytes})
	case "write":
		view, err = s.actions.Write(r.Context(), app.WriteRequest{SessionID: req.Payload.SessionID, InputOffset: req.Payload.InputOffset, Chars: req.Payload.Chars, EOF: req.Payload.EOF})
	case "kill":
		view, err = s.actions.Kill(r.Context(), app.KillRequest{SessionID: req.Payload.SessionID, KillID: req.Payload.KillID, Signal: req.Payload.Signal})
	default:
		err = fmt.Errorf("unknown_action")
	}
	resp := Response{IPVersion: 1, RequestID: req.RequestID, OK: err == nil, View: view}
	if err != nil {
		resp.Error = errorEnvelope(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

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
	switch req.Action {
	case "start":
		view, callErr := s.actions.Start(r.Context(), app.StartRequest{ProtocolVersion: 2, OperationID: req.OperationID, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, WorkspaceHint: req.WorkspaceHint, StructuredAdapter: req.StructuredAdapter, ProjectCommandID: req.ProjectCommandID, Params: cloneStringMapV2(req.Params), Command: req.Command, Argv: append([]string(nil), req.Argv...), Intent: req.Intent, Evidence: req.Evidence, CWD: req.CWD, TTY: req.TTY, Persistent: req.Persistent, SessionName: req.SessionName, TimeoutMS: req.TimeoutMS, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
		err = callErr
		if err == nil {
			result, resultErr := view.StructuredResult()
			err = resultErr
			if resultErr == nil {
				resp.Result = &result
			}
		}
	case "poll":
		view, callErr := s.actions.Poll(r.Context(), app.PollRequest{SessionID: req.SessionID, Cursor: req.Cursor, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
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
		result, callErr := actions.ReadOutputView(r.Context(), outputview.Request{SessionID: req.SessionID, Selector: *req.Selector, Continuation: req.Continuation})
		err = callErr
		if err == nil {
			resp.OutputView = &result
		}
	case "write":
		view, callErr := s.actions.Write(r.Context(), app.WriteRequest{SessionID: req.SessionID, InputOffset: req.InputOffset, Chars: req.Chars, EOF: req.EOF})
		err = callErr
		resp.View = &view
	case "kill":
		view, callErr := s.actions.Kill(r.Context(), app.KillRequest{SessionID: req.SessionID, KillID: req.KillID, Signal: req.Signal})
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
		capsule, callErr := actions.CreateRepro(r.Context(), reprocore.CreateRequest{CreateID: req.ReproCreateID, OperationID: req.OperationID, Policy: policy})
		err = callErr
		if err == nil {
			resp.Capsule = &capsule
		}
	case "inspect.server", "inspect.workspace", "inspect.activity", "inspect.sessions", "inspect.project", "inspect.readiness", "inspect.events", "inspect.structured", "inspect.telemetry", "inspect.evidence", "inspect.environment", "inspect.process", "inspect.repro", "inspect.code", "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		err = s.inspectV2(r.Context(), req, &resp)
	}
	resp.OK = err == nil
	if err != nil {
		clearResponseV2Payload(&resp)
		resp.Error = errorEnvelope(err)
	}
	writeResponseV2(w, resp)
}

func clearResponseV2Payload(resp *ResponseV2) {
	resp.View, resp.Result, resp.Server, resp.Project, resp.Readiness = nil, nil, nil, nil, nil
	resp.Workspace, resp.Activity, resp.Events, resp.Structured, resp.Evidence = nil, nil, nil, nil, nil
	resp.Environment, resp.Process, resp.Mutation, resp.MutationScopes = nil, nil, nil, nil
	resp.ActiveMutationScopes, resp.MutationScopeAdvisories = nil, nil
	resp.MutationScopesTruncated, resp.MutationScopeAdvisoriesTruncated = false, false
	resp.Telemetry, resp.Capsule, resp.Repro, resp.Code, resp.OutputView, resp.Sessions = nil, nil, nil, nil, nil, nil
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

func cloneStringMapV2(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
