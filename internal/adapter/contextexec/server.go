package contextexec

import (
	"context"
	"fmt"
	"net"

	"github.com/maemreyo/shellbeam/internal/core/failure"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type ClaimExpectation struct {
	Identity ClaimIdentity
	Helper   core.HelperBinding
}

func (e ClaimExpectation) Validate() error {
	if err := e.Identity.Validate(); err != nil {
		return err
	}
	if err := e.Helper.Validate(); err != nil {
		return err
	}
	if e.Helper.OpaqueLaunchID != e.Identity.OpaqueLaunchID || e.Helper.Generation != e.Identity.Generation || e.Helper.RequestFingerprint != e.Identity.RequestFingerprint {
		return fmt.Errorf("context helper expectation mismatch")
	}
	return nil
}

type PeerVerifier func(context.Context, net.Conn, ClaimExpectation) error
type ClaimBinder func(context.Context, string, core.HelperBinding, string) (operation.ContextExecState, error)

type Server struct {
	Expectation   ClaimExpectation
	VerifyPeer    PeerVerifier
	BindClaim     ClaimBinder
	NewCapability func() (ClaimCapability, error)
	NewChallenge  func() (string, error)
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
	if err := s.VerifyPeer(ctx, conn, s.Expectation); err != nil {
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
	if s.BindClaim == nil {
		return operation.ContextExecState{}, s.authFailure("claim_binder_missing")
	}
	state, err := s.BindClaim(ctx, s.Expectation.Identity.ContextExecID, s.Expectation.Helper, ClaimVerifierDigest(capability))
	if err != nil {
		return operation.ContextExecState{}, s.authFailure("claim_binding")
	}
	if err := validateBoundState(state, s.Expectation); err != nil {
		return operation.ContextExecState{}, s.authFailure("durable_binding")
	}
	request := RequestFrame{ProtocolVersion: ProtocolVersion, Kind: KindRequest, Request: state.Request, Context: state.Context, Helper: s.Expectation.Helper}
	if err := writeFrame(conn, request); err != nil {
		return operation.ContextExecState{}, err
	}
	return state.Clone(), nil
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
	return nil
}

func (s *Server) authFailure(reason string) error {
	return failure.New(failure.ContextHelperAuthFailed, map[string]string{"context_exec_id": s.Expectation.Identity.ContextExecID, "session_id": s.Expectation.Identity.SessionID, "reason": reason}, nil)
}

type ReceivedResult struct {
	Stdout   []byte
	Stderr   []byte
	Terminal core.Result
}

func (s *Server) ReceiveResult(ctx context.Context, conn net.Conn, state operation.ContextExecState) (ReceivedResult, error) {
	if conn == nil {
		return ReceivedResult{}, fmt.Errorf("context helper connection missing")
	}
	if err := validateBoundState(state, s.Expectation); err != nil {
		return ReceivedResult{}, s.authFailure("durable_binding")
	}
	offsets := map[OutputStream]int64{StreamStdout: 0, StreamStderr: 0}
	var stdout, stderr []byte
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
			if r.ContextExecID != state.Request.ContextExecID || r.RequestFingerprint != state.RequestFingerprint || r.Context != state.Context || r.Helper == nil || state.Helper == nil || *r.Helper != *state.Helper {
				return ReceivedResult{}, fmt.Errorf("context helper terminal identity mismatch")
			}
			if r.Output.StdoutBytes != offsets[StreamStdout] || r.Output.StderrBytes != offsets[StreamStderr] {
				return ReceivedResult{}, fmt.Errorf("context helper terminal output count mismatch")
			}
			return ReceivedResult{Stdout: stdout, Stderr: stderr, Terminal: r}, nil
		}
	}
}
