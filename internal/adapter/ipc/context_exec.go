//go:build linux || darwin

package ipc

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type ContextExecActions interface {
	ExecuteContext(context.Context, contextcore.Request) (operation.ContextExecState, error)
}

func (s *Server) contextExecV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(ContextExecActions)
	if !ok {
		return failure.New(failure.ContextExecUnavailable, map[string]string{"context_exec_id": req.ContextExecID, "session_id": req.SessionID, "reason": "context_exec_unavailable"}, nil)
	}
	request := contextcore.Request{
		ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch,
		Argv: append([]string(nil), req.Argv...), TimeoutMS: req.TimeoutMS, MaxOutputBytes: int64(req.MaxOutputBytes),
	}
	state, err := actions.ExecuteContext(ctx, request)
	if err != nil {
		return err
	}
	public, err := projectContextExecState(state)
	if err != nil {
		return failure.New(failure.InvalidDaemonResponse, nil, err)
	}
	if state.Lifecycle == contextcore.LifecycleCanonicalized && state.Result != nil && state.Result.Spawn.Succeeded && state.ChildSessionID != "" {
		if err := s.enrichContextExecOutput(ctx, req, state, &public); err != nil {
			return err
		}
	}
	resp.ContextExec = &public
	return nil
}

func (s *Server) enrichContextExecOutput(ctx context.Context, req RequestV2, state operation.ContextExecState, public *contextcore.PublicState) error {
	view, err := s.actions.Poll(ctx, app.PollRequest{SessionID: string(state.ChildSessionID), Cursor: 0, MaxOutputBytes: req.MaxOutputBytes})
	if err != nil {
		return err
	}
	if view.SessionID != string(state.ChildSessionID) || view.NextCursor < view.Cursor || view.RawOutputBytes < view.NextCursor {
		return failure.New(failure.InvalidDaemonResponse, nil, fmt.Errorf("invalid context exec output view"))
	}
	if public.Output == nil {
		return failure.New(failure.InvalidDaemonResponse, nil, fmt.Errorf("canonical context exec output evidence missing"))
	}
	public.Output.Preview = view.Output
	public.Output.RawBytes = view.RawOutputBytes
	public.Output.ReturnedBytes = view.NextCursor - view.Cursor
	public.Output.PreviewTruncated = view.Truncated
	return nil
}

func projectContextExecState(state operation.ContextExecState) (contextcore.PublicState, error) {
	if err := state.Validate(); err != nil {
		return contextcore.PublicState{}, fmt.Errorf("invalid context exec state: %w", err)
	}
	out := contextcore.PublicState{
		SchemaVersion: 1, ContextExecID: state.Request.ContextExecID, SessionID: state.Request.SessionID,
		AuthorityEpoch: state.Request.AuthorityEpoch, Lifecycle: state.Lifecycle,
		ChildOperationID: string(state.ChildOperationID), ChildSessionID: string(state.ChildSessionID),
		RequestedExecutable: state.Request.Argv[0],
	}
	if state.Result == nil {
		return out, nil
	}
	result := state.Result
	out.FailureCode = result.FailureCode
	if result.Executable.Requested != "" {
		out.RequestedExecutable = result.Executable.Requested
	}
	out.ResolvedExecutable = result.Executable.ResolvedPath
	spawn, exit, signal := result.Spawn, result.Exit, result.Signal
	out.Spawn, out.Exit, out.Signal = &spawn, &exit, &signal
	timedOut := result.TimedOut
	out.TimedOut = &timedOut
	if result.Output.Attribution != "" {
		out.Output = &contextcore.PublicOutput{
			StdoutBytes: result.Output.StdoutBytes, StderrBytes: result.Output.StderrBytes,
			RawBytes:       result.Output.StdoutBytes + result.Output.StderrBytes,
			OutputComplete: result.Output.OutputComplete, Truncated: result.Output.Truncated,
			Attribution: result.Output.Attribution,
		}
	}
	out.EvidenceQuality = result.EvidenceQuality
	out.EvidenceAuthority = result.EvidenceAuthority
	return out, nil
}
