//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/failure"
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

type EventActions interface {
	InspectEvents(context.Context, observationapp.InspectRequest) (observationapp.InspectResult, error)
}

type StructuredActions interface {
	InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error)
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
		view, callErr := s.actions.Start(r.Context(), app.StartRequest{ProtocolVersion: 2, OperationID: req.OperationID, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, WorkspaceHint: req.WorkspaceHint, StructuredAdapter: req.StructuredAdapter, Command: req.Command, Argv: append([]string(nil), req.Argv...), Intent: req.Intent, CWD: req.CWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
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
	case "inspect.server", "inspect.workspace", "inspect.activity", "inspect.project", "inspect.events", "inspect.structured", "inspect.telemetry", "inspect.repro", "inspect.code":
		err = s.inspectV2(r.Context(), req, &resp)
	}
	resp.OK = err == nil
	if err != nil {
		resp.View = nil
		resp.Result = nil
		resp.Server = nil
		resp.Project = nil
		resp.Workspace = nil
		resp.Activity = nil
		resp.Events = nil
		resp.Structured = nil
		resp.Telemetry = nil
		resp.Capsule = nil
		resp.Repro = nil
		resp.Code = nil
		resp.Error = errorEnvelope(err)
	}
	writeResponseV2(w, resp)
}

func (s *Server) inspectV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	switch req.Action {
	case "inspect.server":
		info, err := s.actions.InspectServer(ctx)
		catalog := info.Capabilities
		resp.Server = &catalog
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
		actions, ok := s.actions.(ActivityActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		record, err := actions.InspectActivity(ctx, req.ActivityID)
		resp.Activity = &record
		return err
	case "inspect.project":
		actions, ok := s.actions.(ProjectActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		inspection, err := actions.InspectProject(ctx, req.WorkspaceID)
		resp.Project = &inspection
		return err
	case "inspect.structured":
		actions, ok := s.actions.(StructuredActions)
		if !ok {
			return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
		}
		request := structuredapp.InspectRequest{OperationID: req.OperationID, Filter: structuredapp.RecordFilter{RecordKind: req.RecordKind, Severity: req.Severity, Path: req.Path, TestStatus: req.TestStatus}, Continuation: req.Continuation, MaxRecords: req.MaxRecords}
		result, err := actions.InspectStructured(ctx, request)
		resp.Structured = &result
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

func writeResponseV2(w http.ResponseWriter, response ResponseV2) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
