package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type Server struct {
	runtime    *Runtime
	capability Capability
	sessionID  string
	generation string
}

func NewServer(runtime *Runtime, capability Capability) (*Server, error) {
	if runtime == nil {
		return nil, fmt.Errorf("supervisor runtime missing")
	}
	status := runtime.Status()
	if status.SessionID == "" || status.GenerationID == "" {
		return nil, fmt.Errorf("supervisor runtime identity missing")
	}
	return &Server{runtime: runtime, capability: capability, sessionID: status.SessionID, generation: status.GenerationID}, nil
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if listener == nil {
		return fmt.Errorf("supervisor listener missing")
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go func() { _ = s.ServeConn(ctx, conn) }()
	}
}

func (s *Server) ServeConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, MaxFrameBytes+1)
	writer := bufio.NewWriterSize(conn, MaxFrameBytes+1)
	request, err := readRequestFrame(reader)
	if err != nil {
		return err
	}
	if request.Kind != KindHandshake || request.SessionID != s.sessionID || request.GenerationID != s.generation || !VerifyProof(s.capability, request.SessionID, request.GenerationID, request.Handshake.Challenge, request.Handshake.Proof) {
		authErr := failure.New(failure.SupervisorAuthFailed, map[string]string{"session_id": s.sessionID, "reason": "handshake"}, fmt.Errorf("supervisor authentication failed"))
		_ = s.write(writer, responseError(KindHandshake, s.sessionID, s.generation, authErr))
		return authErr
	}
	if err := s.write(writer, Response{ProtocolVersion: ProtocolVersion, Kind: KindHandshake, SessionID: s.sessionID, GenerationID: s.generation, OK: true, Authenticated: true, Status: statusResponse(s.runtime.Status())}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		request, err = readRequestFrame(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if request.SessionID != s.sessionID || request.GenerationID != s.generation || request.Kind == KindHandshake {
			identityErr := failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": s.sessionID, "reason": "identity"}, fmt.Errorf("supervisor identity mismatch"))
			_ = s.write(writer, responseError(request.Kind, s.sessionID, s.generation, identityErr))
			return identityErr
		}
		response := s.dispatch(ctx, request)
		if err := s.write(writer, response); err != nil {
			return err
		}
	}
}

func (s *Server) dispatch(ctx context.Context, request Request) Response {
	base := Response{ProtocolVersion: ProtocolVersion, Kind: request.Kind, SessionID: s.sessionID, GenerationID: s.generation, OK: true}
	switch request.Kind {
	case KindStatus:
		base.Status = statusResponse(s.runtime.Status())
	case KindOutput:
		data, extent, err := s.runtime.Output(request.Output.Offset, request.Output.MaxBytes)
		if err != nil {
			return responseError(request.Kind, s.sessionID, s.generation, err)
		}
		base.Output = &OutputResponse{Offset: request.Output.Offset, NextOffset: request.Output.Offset + int64(len(data)), Extent: extent, Data: data}
	case KindWrite:
		admission, err := s.runtime.Write(request.Write.InputOffset, []byte(request.Write.Chars), request.Write.EOF)
		if err != nil {
			return responseError(request.Kind, s.sessionID, s.generation, err)
		}
		base.Write = &WriteResponse{AcceptedBytes: admission.AcceptedBytes, NextOffset: admission.NextOffset, Duplicate: admission.Duplicate, EOFDelivered: s.runtime.Status().Input.EOFDelivered}
	case KindSignal:
		attempt, err := s.runtime.Signal(request.Signal.KillID, request.Signal.Signal)
		if err != nil {
			return responseError(request.Kind, s.sessionID, s.generation, err)
		}
		base.Signal = &attempt
	case KindWait:
		status, err := s.waitStatus(ctx, request.Wait.AfterChange, request.Wait.WaitMS)
		if err != nil {
			return responseError(request.Kind, s.sessionID, s.generation, err)
		}
		base.Status = statusResponse(status)
	default:
		return responseError(request.Kind, s.sessionID, s.generation, protocolFailure("kind"))
	}
	return base
}

func (s *Server) waitStatus(ctx context.Context, after uint64, waitMS int) (RuntimeStatus, error) {
	current := s.runtime.Status()
	if current.Change > after || waitMS == 0 {
		return current, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(waitMS)*time.Millisecond)
	defer cancel()
	status, err := s.runtime.WaitChange(waitCtx, after)
	if errors.Is(err, context.DeadlineExceeded) {
		return s.runtime.Status(), nil
	}
	return status, err
}

func (s *Server) write(writer *bufio.Writer, response Response) error {
	if err := writeResponse(writer, response); err != nil {
		return err
	}
	return writer.Flush()
}
