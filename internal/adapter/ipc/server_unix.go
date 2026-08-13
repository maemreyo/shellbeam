//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Actions interface {
	Start(context.Context, app.StartRequest) (app.View, error)
	Poll(context.Context, app.PollRequest) (app.View, error)
	Write(context.Context, app.WriteRequest) (app.View, error)
	Kill(context.Context, app.KillRequest) (app.View, error)
	InspectServer(context.Context) (app.ServerInfo, error)
}

type socketDialer func(string, time.Duration) (net.Conn, error)

type Server struct {
	socket     string
	socketInfo os.FileInfo
	listener   net.Listener
	http       *http.Server
	actions    Actions
}

func Listen(runtime string, actions Actions) (*Server, error) {
	return listen(runtime, actions, dialUnixSocket)
}

func listen(runtime string, actions Actions, dial socketDialer) (*Server, error) {
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
	s := &Server{socket: socket, socketInfo: socketInfo, listener: auth, actions: actions}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/local-shell", s.handle)
	mux.HandleFunc("POST /v2/local-shell", s.handleV2)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	return s, nil
}

func prepareRuntime(runtime string) error {
	if !filepath.IsAbs(runtime) {
		return fmt.Errorf("runtime path must be absolute")
	}
	if err := os.MkdirAll(runtime, 0700); err != nil {
		return err
	}
	if err := os.Chmod(runtime, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(runtime)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe runtime directory")
	}
	return nil
}

func claimSocket(socket string, dial socketDialer) (net.Listener, os.FileInfo, error) {
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("unsafe socket collision")
		}
		if err := removeStaleSocket(socket, info, dial); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, nil, err
	}
	if unixListener, ok := ln.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	if err = os.Chmod(socket, 0600); err != nil {
		_ = ln.Close()
		_ = os.Remove(socket)
		return nil, nil, err
	}
	info, err := os.Lstat(socket)
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(socket)
		return nil, nil, err
	}
	return ln, info, nil
}

func removeStaleSocket(socket string, expected os.FileInfo, dial socketDialer) error {
	conn, err := dial(socket, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon_already_running")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("daemon_already_running")
	}
	current, err := os.Lstat(socket)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !os.SameFile(expected, current) {
		return fmt.Errorf("daemon_already_running")
	}
	return os.Remove(socket)
}

func dialUnixSocket(socket string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socket, timeout)
}

func (s *Server) SocketPath() string { return s.socket }
func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func (s *Server) Close() error {
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
	resp := ResponseV2{IPVersion: ipcV2, Kind: "response", RequestID: req.RequestID, Action: req.Action}
	switch req.Action {
	case "start":
		view, callErr := s.actions.Start(r.Context(), app.StartRequest{OperationID: req.OperationID, Command: req.Command, CWD: req.CWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
		err = callErr
		resp.View = &view
	case "poll":
		view, callErr := s.actions.Poll(r.Context(), app.PollRequest{SessionID: req.SessionID, Cursor: req.Cursor, YieldMS: req.YieldMS, MaxOutputBytes: req.MaxOutputBytes})
		err = callErr
		resp.View = &view
	case "write":
		view, callErr := s.actions.Write(r.Context(), app.WriteRequest{SessionID: req.SessionID, InputOffset: req.InputOffset, Chars: req.Chars, EOF: req.EOF})
		err = callErr
		resp.View = &view
	case "kill":
		view, callErr := s.actions.Kill(r.Context(), app.KillRequest{SessionID: req.SessionID, KillID: req.KillID, Signal: req.Signal})
		err = callErr
		resp.View = &view
	case "inspect.server":
		info, callErr := s.actions.InspectServer(r.Context())
		err = callErr
		catalog := info.Capabilities
		resp.Server = &catalog
	}
	resp.OK = err == nil
	if err != nil {
		resp.View = nil
		resp.Server = nil
		resp.Error = errorEnvelope(err)
	}
	writeResponseV2(w, resp)
}

func writeResponseV2(w http.ResponseWriter, response ResponseV2) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
