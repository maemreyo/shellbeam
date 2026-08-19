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
	// lease is this daemon's ownership of the runtime directory. It outlives
	// the socket pathname deliberately and is surrendered only on Close.
	lease     RuntimeLease
	ready     chan struct{}
	readyOnce sync.Once
	closing   chan struct{}
	closeOnce sync.Once
}

// ListenPendingWithLease starts a pending server using an already-acquired
// runtime-directory lease. The server owns the supplied lease immediately: it
// releases it on setup failure or, after successful construction, on Close.
func ListenPendingWithLease(runtime string, actions Actions, lease RuntimeLease) (*Server, error) {
	return listenWithReadiness(runtime, actions, lease, dialUnixSocket, false)
}

func listenWithReadiness(runtime string, actions Actions, lease RuntimeLease, dial socketDialer, ready bool) (*Server, error) {
	if lease == nil {
		return nil, fmt.Errorf("runtime ownership lease required")
	}
	if err := prepareRuntime(runtime); err != nil {
		_ = lease.Release()
		return nil, err
	}
	released := false
	defer func() {
		if !released {
			_ = lease.Release()
		}
	}()
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
		lease: lease, ready: make(chan struct{}), closing: make(chan struct{}),
	}
	released = true
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/local-shell", s.handle)
	mux.HandleFunc("POST /v2/local-shell", s.handleV2)
	mux.HandleFunc("POST /local/handoff", s.handleHandoffLocal)
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
	// Ownership is surrendered last: until the socket is withdrawn this daemon
	// is still the endpoint, and a successor must not be able to start while
	// that is true.
	if leaseErr := s.lease.Release(); err == nil && leaseErr != nil {
		err = leaseErr
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
	resp := Response{IPVersion: 1, RequestID: req.RequestID, OK: err == nil, View: legacyResponseView(view)}
	if err != nil {
		resp.Error = errorEnvelope(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
