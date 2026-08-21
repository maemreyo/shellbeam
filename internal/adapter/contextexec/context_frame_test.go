package contextexec

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestContextFrameCarriesOnlyAuthenticatedAbsoluteCWD(t *testing.T) {
	frame := ContextFrame{ProtocolVersion: ProtocolVersion, Kind: KindContext, CWD: "/tmp/project"}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { done <- writeFrame(left, frame) }()
	got, err := readContextFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got != frame {
		t.Fatalf("context frame=%#v", got)
	}
	for _, cwd := range []string{"", "relative/path"} {
		bad := frame
		bad.CWD = cwd
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted cwd %q", cwd)
		}
	}
}

func TestServerRejectsAuthenticatedCWDMismatchBeforeBindingOrRequestDelivery(t *testing.T) {
	expectation := validClaimExpectation(t)
	expectation.Context = validContextExpectation(expectation)
	var bindCalls atomic.Int32
	server := &Server{
		Expectation: expectation,
		VerifyPeer:  func(context.Context, net.Conn, ClaimExpectation) error { return nil },
		BindClaim: func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
			bindCalls.Add(1)
			return operation.ContextExecState{}, errors.New("binder must not run")
		},
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()

	challenge, capability := completeHelloAndReadChallenge(t, right, expectation)
	proof, err := ClaimProof(capability, expectation.Identity, challenge.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(right, ProofFrame{ProtocolVersion: ProtocolVersion, Kind: KindProof, Proof: proof}); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(right, ContextFrame{ProtocolVersion: ProtocolVersion, Kind: KindContext, CWD: "/tmp/other"}); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Code != failure.ContextExecBoundaryUnproven {
		t.Fatalf("err=%#v", err)
	}
	if bindCalls.Load() != 0 {
		t.Fatalf("binder calls=%d", bindCalls.Load())
	}
}

func TestServerBindsFinalPromptContextAtomicallyBeforeSendingRequest(t *testing.T) {
	expectation := validClaimExpectation(t)
	expectation.Context = validContextExpectation(expectation)
	observedAt := time.Date(2026, 8, 21, 11, 30, 0, 0, time.UTC)
	var bindCalls atomic.Int32
	server := &Server{
		Expectation: expectation,
		VerifyPeer:  func(context.Context, net.Conn, ClaimExpectation) error { return nil },
		Now:         func() time.Time { return observedAt },
		BindClaim: func(_ context.Context, id string, helper core.HelperBinding, final core.ContextBinding, boundary time.Time, digest string) (operation.ContextExecState, error) {
			bindCalls.Add(1)
			if id != expectation.Identity.ContextExecID || helper != expectation.Helper || len(digest) != 64 || boundary != observedAt {
				return operation.ContextExecState{}, errors.New("binding metadata mismatch")
			}
			want := core.ContextBinding{
				SessionID: expectation.Context.SessionID, AuthorityEpoch: expectation.Context.AuthorityEpoch,
				ShellIdentity: expectation.Context.ShellIdentity, BoundaryQuality: "shell_prompt",
				CWDObserved: expectation.Context.CWDObserved, PrivacyState: expectation.Context.PrivacyState,
			}
			if final != want {
				return operation.ContextExecState{}, errors.New("final context mismatch")
			}
			state := validBoundState(t, expectation)
			state.Context = &final
			return state, nil
		},
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()

	client := Client{Conn: right, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID, Getwd: func() (string, error) { return expectation.Context.CWDObserved, nil }}
	req, err := client.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if bindCalls.Load() != 1 || req.Context.BoundaryQuality != "shell_prompt" || req.Context.CWDObserved != expectation.Context.CWDObserved {
		t.Fatalf("binds=%d request=%#v", bindCalls.Load(), req)
	}
}

func validContextExpectation(expectation ClaimExpectation) core.ContextExpectation {
	return core.ContextExpectation{
		SessionID:          expectation.Identity.SessionID,
		AuthorityEpoch:     expectation.Identity.AuthorityEpoch,
		ProviderGeneration: "gen_task5a_01",
		ShellIdentity:      "fish:runtime_01",
		CWDObserved:        "/tmp/project",
		PrivacyState:       "standard",
	}
}

func completeHelloAndReadChallenge(t *testing.T, conn net.Conn, expectation ClaimExpectation) (ChallengeFrame, ClaimCapability) {
	t.Helper()
	if err := writeFrame(conn, HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID}); err != nil {
		t.Fatal(err)
	}
	challenge, err := readChallengeFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := readCapability(conn)
	if err != nil {
		t.Fatal(err)
	}
	return challenge, capability
}
