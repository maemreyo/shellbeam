package contextexec

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type ClaimExpectation struct {
	Identity   ClaimIdentity
	Helper     core.HelperBinding
	Context    core.ContextExpectation
	Continuity core.ShellContinuityExpectation
}

func (e ClaimExpectation) Validate() error {
	if err := e.Identity.Validate(); err != nil {
		return err
	}
	if err := e.Helper.Validate(); err != nil {
		return err
	}
	if err := e.Context.Validate(); err != nil {
		return err
	}
	if err := e.Continuity.Validate(); err != nil {
		return err
	}
	if e.Context.SessionID != e.Identity.SessionID || e.Context.AuthorityEpoch != e.Identity.AuthorityEpoch {
		return fmt.Errorf("context helper expectation authority mismatch")
	}
	if e.Helper.OpaqueLaunchID != e.Identity.OpaqueLaunchID || e.Helper.Generation != e.Identity.Generation || e.Helper.RequestFingerprint != e.Identity.RequestFingerprint {
		return fmt.Errorf("context helper expectation mismatch")
	}
	if e.Continuity.SessionID != e.Identity.SessionID ||
		e.Continuity.AuthorityEpoch != e.Identity.AuthorityEpoch ||
		e.Continuity.ProviderGeneration != e.Context.ProviderGeneration ||
		e.Continuity.ShellRuntimeIdentity != e.Context.ShellIdentity ||
		filepath.Clean(e.Continuity.HelperExecutableIdentity) != filepath.Clean(e.Helper.ExecutablePath) {
		return fmt.Errorf("context helper continuity expectation mismatch")
	}
	return nil
}

type PeerVerifier func(context.Context, net.Conn, ClaimExpectation) (core.ShellContinuityProof, error)
type ClaimBinder func(context.Context, string, core.HelperBinding, core.ContextBinding, core.ShellContinuityExpectation, core.ShellContinuityProof, time.Time, string) (operation.ContextExecState, error)
type PreparedAuthorizer func(context.Context, operation.ContextExecState, PreparedFrame) (operation.ContextExecState, ExecuteFrame, error)
type SpawnRecorder func(context.Context, operation.ContextExecState, SpawnFrame) (operation.ContextExecState, error)

type Server struct {
	Expectation       ClaimExpectation
	VerifyPeer        PeerVerifier
	BindClaim         ClaimBinder
	NewCapability     func() (ClaimCapability, error)
	NewChallenge      func() (string, error)
	Now               func() time.Time
	AuthorizePrepared PreparedAuthorizer
	RecordSpawn       SpawnRecorder
}

func (s *Server) Authenticate(ctx context.Context, conn net.Conn) (operation.ContextExecState, error) {
	if conn == nil {
		return operation.ContextExecState{}, fmt.Errorf("context helper connection missing")
	}
	if err := s.Expectation.Validate(); err != nil {
		return operation.ContextExecState{}, err
	}
	hello, err := readHelloFrame(conn)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if hello.OpaqueLaunchID != s.Expectation.Identity.OpaqueLaunchID {
		return operation.ContextExecState{}, s.authFailure("launch_identity")
	}
	if s.VerifyPeer == nil {
		return operation.ContextExecState{}, s.authFailure("peer_verifier_missing")
	}
	continuityProof, err := s.VerifyPeer(ctx, conn, s.Expectation)
	if err != nil {
		return operation.ContextExecState{}, s.authFailure("peer_unproven")
	}
	if err := continuityProof.ValidateFor(s.Expectation.Continuity); err != nil {
		return operation.ContextExecState{}, s.authFailure("peer_unproven")
	}
	capFn := s.NewCapability
	if capFn == nil {
		capFn = NewClaimCapability
	}
	challengeFn := s.NewChallenge
	if challengeFn == nil {
		challengeFn = NewClaimChallenge
	}
	capability, err := capFn()
	if err != nil {
		return operation.ContextExecState{}, err
	}
	challenge, err := challengeFn()
	if err != nil {
		return operation.ContextExecState{}, err
	}
	challengeFrame := ChallengeFrame{ProtocolVersion: ProtocolVersion, Kind: KindChallenge, Identity: s.Expectation.Identity, Challenge: challenge}
	if err := writeFrame(conn, challengeFrame); err != nil {
		return operation.ContextExecState{}, err
	}
	if err := writeCapability(conn, capability); err != nil {
		return operation.ContextExecState{}, err
	}
	proof, err := readProofFrame(conn)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if !VerifyClaimProof(capability, s.Expectation.Identity, challenge, proof.Proof) {
		return operation.ContextExecState{}, s.authFailure("proof")
	}
	contextFrame, err := readContextFrame(conn)
	if err != nil {
		return operation.ContextExecState{}, err
	}
	if !sameDirectoryIdentity(contextFrame.CWD, s.Expectation.Context.CWDObserved) {
		return operation.ContextExecState{}, s.boundaryFailure("cwd_mismatch")
	}
	if s.BindClaim == nil {
		return operation.ContextExecState{}, s.authFailure("claim_binder_missing")
	}
	finalContext := finalContextBinding(s.Expectation.Context)
	now := s.boundaryNow()
	state, err := s.BindClaim(ctx, s.Expectation.Identity.ContextExecID, s.Expectation.Helper, finalContext, s.Expectation.Continuity, continuityProof, now, ClaimVerifierDigest(capability))
	if err != nil {
		return operation.ContextExecState{}, s.authFailure("claim_binding")
	}
	if err := validateBoundState(state, s.Expectation); err != nil {
		return operation.ContextExecState{}, s.authFailure("durable_binding")
	}
	request := RequestFrame{ProtocolVersion: ProtocolVersion, Kind: KindRequest, Request: state.Request, Context: *state.Context, Helper: s.Expectation.Helper}
	if err := writeFrame(conn, request); err != nil {
		return operation.ContextExecState{}, err
	}
	return state.Clone(), nil
}
func sameDirectoryIdentity(observed, expected string) bool {
	observed = filepath.Clean(observed)
	expected = filepath.Clean(expected)
	if observed == expected {
		return true
	}
	observedInfo, err := os.Stat(observed)
	if err != nil || !observedInfo.IsDir() {
		return false
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil || !expectedInfo.IsDir() {
		return false
	}
	return os.SameFile(observedInfo, expectedInfo)
}

func finalContextBinding(expectation core.ContextExpectation) core.ContextBinding {
	return core.ContextBinding{
		SessionID: expectation.SessionID, AuthorityEpoch: expectation.AuthorityEpoch,
		ShellIdentity: expectation.ShellIdentity, BoundaryQuality: "shell_prompt",
		CWDObserved: expectation.CWDObserved, PrivacyState: expectation.PrivacyState,
	}
}

func (s *Server) boundaryNow() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func validateBoundState(state operation.ContextExecState, e ClaimExpectation) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if state.Lifecycle != core.LifecycleHelperAuthenticated || state.Helper == nil || *state.Helper != e.Helper {
		return fmt.Errorf("context helper durable binding mismatch")
	}
	if state.Request.ContextExecID != e.Identity.ContextExecID || state.Request.SessionID != e.Identity.SessionID || state.Request.AuthorityEpoch != e.Identity.AuthorityEpoch || state.RequestFingerprint != e.Identity.RequestFingerprint {
		return fmt.Errorf("context helper durable request mismatch")
	}
	wantContext := core.ContextBinding{SessionID: e.Context.SessionID, AuthorityEpoch: e.Context.AuthorityEpoch, ShellIdentity: e.Context.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: e.Context.CWDObserved, PrivacyState: e.Context.PrivacyState}
	if state.Context == nil || *state.Context != wantContext {
		return fmt.Errorf("context helper durable context mismatch")
	}
	return nil
}

func (s *Server) authFailure(reason string) error {
	return failure.New(failure.ContextHelperAuthFailed, map[string]string{"context_exec_id": s.Expectation.Identity.ContextExecID, "session_id": s.Expectation.Identity.SessionID, "reason": reason}, nil)
}

func (s *Server) boundaryFailure(reason string) error {
	return failure.New(failure.ContextExecBoundaryUnproven, map[string]string{"context_exec_id": s.Expectation.Identity.ContextExecID, "session_id": s.Expectation.Identity.SessionID, "reason": reason}, nil)
}

func (s *Server) ReceiveExecution(ctx context.Context, conn net.Conn, state operation.ContextExecState) (operation.ContextExecState, ReceivedResult, error) {
	if conn == nil {
		return operation.ContextExecState{}, ReceivedResult{}, fmt.Errorf("context helper connection missing")
	}
	if err := validateExecutionState(state, s.Expectation, core.LifecycleHelperAuthenticated); err != nil {
		return operation.ContextExecState{}, ReceivedResult{}, s.authFailure("durable_binding")
	}
	if s.AuthorizePrepared == nil {
		return state.Clone(), ReceivedResult{}, fmt.Errorf("context helper prepared authorizer unavailable")
	}
	prepared, err := readPreparedFrame(conn)
	if err != nil {
		return state.Clone(), ReceivedResult{}, err
	}
	next, execute, err := s.AuthorizePrepared(ctx, state.Clone(), prepared)
	if err != nil {
		return state.Clone(), ReceivedResult{}, err
	}
	if err := execute.Validate(); err != nil {
		return state.Clone(), ReceivedResult{}, err
	}
	if prepared.FailureCode != "" {
		if execute.Authorized {
			return state.Clone(), ReceivedResult{}, fmt.Errorf("context helper prepare failure was authorized")
		}
		if err := writeFrame(conn, execute); err != nil {
			return next.Clone(), ReceivedResult{}, err
		}
		return next.Clone(), ReceivedResult{}, nil
	}
	if !execute.Authorized {
		return state.Clone(), ReceivedResult{}, fmt.Errorf("prepared context child denied without deterministic failure")
	}
	if err := validateExecutionState(next, s.Expectation, core.LifecycleChildReserved); err != nil || !next.ExecutionAuthorized || next.ChildOperationID != execute.ChildOperationID || next.ChildSessionID != execute.ChildSessionID || !sameExecutable(execute.ResolvedExecutable, prepared.ResolvedExecutable) {
		if err != nil {
			return state.Clone(), ReceivedResult{}, err
		}
		return state.Clone(), ReceivedResult{}, fmt.Errorf("execute authorization lacks durable child reservation")
	}
	if err := writeFrame(conn, execute); err != nil {
		return next.Clone(), ReceivedResult{}, err
	}
	spawn, err := readSpawnFrame(conn)
	if err != nil {
		return next.Clone(), ReceivedResult{}, err
	}
	if spawn.ChildOperationID != execute.ChildOperationID || spawn.ChildSessionID != execute.ChildSessionID || !sameExecutable(spawn.ResolvedExecutable, prepared.ResolvedExecutable) {
		return next.Clone(), ReceivedResult{}, fmt.Errorf("context helper spawn identity mismatch")
	}
	if s.RecordSpawn == nil {
		return next.Clone(), ReceivedResult{}, fmt.Errorf("context helper spawn recorder unavailable")
	}
	spawned, err := s.RecordSpawn(ctx, next.Clone(), spawn)
	if err != nil {
		return next.Clone(), ReceivedResult{}, err
	}
	if !spawn.Spawn.Succeeded {
		return spawned.Clone(), ReceivedResult{}, nil
	}
	if err := validateExecutionState(spawned, s.Expectation, core.LifecycleChildSpawned); err != nil || !spawned.ExecutionAuthorized || spawned.ChildOperationID != execute.ChildOperationID || spawned.ChildSessionID != execute.ChildSessionID {
		if err != nil {
			return next.Clone(), ReceivedResult{}, err
		}
		return next.Clone(), ReceivedResult{}, fmt.Errorf("spawn truth lacks durable child state")
	}
	result, err := s.receiveResult(ctx, conn, spawned)
	if err != nil {
		return spawned.Clone(), ReceivedResult{}, err
	}
	return spawned.Clone(), result, nil
}

func validateExecutionState(state operation.ContextExecState, e ClaimExpectation, lifecycle core.Lifecycle) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if state.Lifecycle != lifecycle || state.Helper == nil || *state.Helper != e.Helper || state.Context == nil {
		return fmt.Errorf("context helper durable execution binding mismatch")
	}
	if state.Request.ContextExecID != e.Identity.ContextExecID || state.Request.SessionID != e.Identity.SessionID || state.Request.AuthorityEpoch != e.Identity.AuthorityEpoch || state.RequestFingerprint != e.Identity.RequestFingerprint {
		return fmt.Errorf("context helper durable execution request mismatch")
	}
	wantContext := core.ContextBinding{SessionID: e.Context.SessionID, AuthorityEpoch: e.Context.AuthorityEpoch, ShellIdentity: e.Context.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: e.Context.CWDObserved, PrivacyState: e.Context.PrivacyState}
	if *state.Context != wantContext {
		return fmt.Errorf("context helper durable execution context mismatch")
	}
	return nil
}

type ReceivedResult struct {
	Stdout   []byte
	Stderr   []byte
	Combined []byte
	Terminal core.Result
}

func (s *Server) ReceiveResult(ctx context.Context, conn net.Conn, state operation.ContextExecState) (ReceivedResult, error) {
	if err := validateExecutionState(state, s.Expectation, core.LifecycleHelperAuthenticated); err != nil {
		return ReceivedResult{}, s.authFailure("durable_binding")
	}
	return s.receiveResult(ctx, conn, state)
}

func (s *Server) receiveResult(ctx context.Context, conn net.Conn, state operation.ContextExecState) (ReceivedResult, error) {
	if conn == nil {
		return ReceivedResult{}, fmt.Errorf("context helper connection missing")
	}
	offsets := map[OutputStream]int64{StreamStdout: 0, StreamStderr: 0}
	var stdout, stderr, combined []byte
	for {
		if err := ctx.Err(); err != nil {
			return ReceivedResult{}, err
		}
		raw, err := readRawFrame(conn)
		if err != nil {
			return ReceivedResult{}, err
		}
		kind, err := frameKind(raw)
		if err != nil {
			return ReceivedResult{}, err
		}
		switch kind {
		case KindOutput:
			var frame OutputFrame
			if err := decodeTypedFrame(raw, &frame); err != nil {
				return ReceivedResult{}, err
			}
			if frame.Offset != offsets[frame.Stream] {
				return ReceivedResult{}, fmt.Errorf("context helper output offset conflict")
			}
			if offsets[StreamStdout]+offsets[StreamStderr]+int64(len(frame.Data)) > state.Request.MaxOutputBytes {
				return ReceivedResult{}, fmt.Errorf("context helper output bound exceeded")
			}
			offsets[frame.Stream] += int64(len(frame.Data))
			combined = append(combined, frame.Data...)
			if frame.Stream == StreamStdout {
				stdout = append(stdout, frame.Data...)
			} else {
				stderr = append(stderr, frame.Data...)
			}
		case KindTerminal:
			var frame TerminalFrame
			if err := decodeTypedFrame(raw, &frame); err != nil {
				return ReceivedResult{}, err
			}
			r := frame.Result
			if r.Lifecycle != core.LifecycleChildTerminal || r.EvidenceAuthority != "" {
				return ReceivedResult{}, fmt.Errorf("context helper terminal attempted daemon authority")
			}
			if r.ContextExecID != state.Request.ContextExecID || r.RequestFingerprint != state.RequestFingerprint || state.Context == nil || r.Context != *state.Context || r.Helper == nil || state.Helper == nil || *r.Helper != *state.Helper {
				return ReceivedResult{}, fmt.Errorf("context helper terminal identity mismatch")
			}
			if r.Output.StdoutBytes != offsets[StreamStdout] || r.Output.StderrBytes != offsets[StreamStderr] {
				return ReceivedResult{}, fmt.Errorf("context helper terminal output count mismatch")
			}
			return ReceivedResult{Stdout: stdout, Stderr: stderr, Combined: combined, Terminal: r}, nil
		}
	}
}
