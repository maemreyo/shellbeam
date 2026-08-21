package contextexec

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestClaimCapabilityIsPrivateAndProofBindsEveryAuthorityField(t *testing.T) {
	cap, err := NewClaimCapability()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", cap, cap), "[") == false { /* redaction text is implementation-defined but non-empty */
	}
	raw := cap.bytes()
	if len(raw) != ClaimCapabilityBytes {
		t.Fatalf("len=%d", len(raw))
	}
	if strings.Contains(fmt.Sprintf("%v", cap), string(raw)) {
		t.Fatal("capability printable")
	}
	var wire bytes.Buffer
	if err := writeCapability(&wire, cap); err != nil {
		t.Fatal(err)
	}
	decoded, err := readCapability(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if !cap.equal(decoded) {
		t.Fatal("capability roundtrip mismatch")
	}

	identity := validClaimIdentity(t)
	challenge, err := NewClaimChallenge()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ClaimProof(cap, identity, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyClaimProof(cap, identity, challenge, proof) {
		t.Fatal("valid proof rejected")
	}
	mutations := []func(*ClaimIdentity){
		func(v *ClaimIdentity) { v.OpaqueLaunchID += "x" },
		func(v *ClaimIdentity) { v.ContextExecID += "x" },
		func(v *ClaimIdentity) { v.SessionID += "x" },
		func(v *ClaimIdentity) { v.AuthorityEpoch++ },
		func(v *ClaimIdentity) { v.Generation += "x" },
		func(v *ClaimIdentity) { v.RequestFingerprint = strings.Repeat("b", 64) },
	}
	for i, mutate := range mutations {
		changed := identity
		mutate(&changed)
		if VerifyClaimProof(cap, changed, challenge, proof) {
			t.Fatalf("mutation %d preserved proof", i)
		}
	}
	nextChallenge, _ := NewClaimChallenge()
	if VerifyClaimProof(cap, identity, nextChallenge, proof) {
		t.Fatal("proof replayed across challenge")
	}
	if digest := ClaimVerifierDigest(cap); len(digest) != 64 || strings.Contains(digest, string(raw)) {
		t.Fatalf("unsafe verifier digest %q", digest)
	}
}

func TestServerRejectsCorrelationOnlyUnsafePeerAndWrongProofBeforeRequestFetch(t *testing.T) {
	expectation := validClaimExpectation(t)
	cases := []struct {
		name         string
		peerErr      error
		corruptProof bool
	}{
		{name: "unsafe_peer", peerErr: errors.New("unsafe peer")},
		{name: "wrong_proof", corruptProof: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bindCalls atomic.Int32
			server := &Server{
				Expectation: expectation,
				VerifyPeer:  func(context.Context, net.Conn, ClaimExpectation) error { return tc.peerErr },
				BindClaim: func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
					bindCalls.Add(1)
					return validBoundState(t, expectation), nil
				},
			}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			done := make(chan error, 1)
			go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()
			if err := writeFrame(right, HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID}); err != nil {
				t.Fatal(err)
			}
			if tc.peerErr == nil {
				challenge, err := readChallengeFrame(right)
				if err != nil {
					t.Fatal(err)
				}
				cap, err := readCapability(right)
				if err != nil {
					t.Fatal(err)
				}
				proof, err := ClaimProof(cap, expectation.Identity, challenge.Challenge)
				if err != nil {
					t.Fatal(err)
				}
				if tc.corruptProof {
					proof = strings.Repeat("x", len(proof))
				}
				if err := writeFrame(right, ProofFrame{ProtocolVersion: ProtocolVersion, Kind: KindProof, Proof: proof}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("unauthorized helper authenticated")
				}
			case <-time.After(time.Second):
				t.Fatal("auth hung")
			}
			if bindCalls.Load() != 0 {
				t.Fatalf("request/binder reached before auth: %d", bindCalls.Load())
			}
		})
	}
}

func TestServerAuthenticatesOneBoundGenerationThenSendsRequest(t *testing.T) {
	expectation := validClaimExpectation(t)
	var bindCalls atomic.Int32
	server := &Server{
		Expectation: expectation,
		VerifyPeer:  func(context.Context, net.Conn, ClaimExpectation) error { return nil },
		BindClaim: func(_ context.Context, id string, helper core.HelperBinding, _ core.ContextBinding, _ time.Time, digest string) (operation.ContextExecState, error) {
			bindCalls.Add(1)
			if id != expectation.Identity.ContextExecID || helper != expectation.Helper || len(digest) != 64 {
				return operation.ContextExecState{}, errors.New("binding mismatch")
			}
			return validBoundState(t, expectation), nil
		},
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()
	client := Client{Conn: right, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID, Getwd: func() (string, error) { return expectation.Context.CWDObserved, nil }}
	request, err := client.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if request.Request.ContextExecID != expectation.Identity.ContextExecID || request.Helper.Generation != expectation.Helper.Generation {
		t.Fatalf("request=%#v", request)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if bindCalls.Load() != 1 {
		t.Fatalf("bind calls=%d", bindCalls.Load())
	}
}

func validClaimIdentity(t *testing.T) ClaimIdentity {
	t.Helper()
	req := core.Request{ContextExecID: "ctxexec_01", SessionID: "session_01", AuthorityEpoch: 4, Argv: []string{"printf", "ok"}, TimeoutMS: 1000, MaxOutputBytes: 1024}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return ClaimIdentity{OpaqueLaunchID: "launch_01", ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, Generation: "generation_01", RequestFingerprint: fp}
}
func validClaimExpectation(t *testing.T) ClaimExpectation {
	t.Helper()
	id := validClaimIdentity(t)
	return ClaimExpectation{Identity: id, Helper: core.HelperBinding{OpaqueLaunchID: id.OpaqueLaunchID, Generation: id.Generation, RequestFingerprint: id.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}, Context: core.ContextExpectation{SessionID: id.SessionID, AuthorityEpoch: id.AuthorityEpoch, ProviderGeneration: "gen_task5a_01", ShellIdentity: "fish:runtime_01", CWDObserved: "/tmp/project", PrivacyState: "standard"}}
}
func validBoundState(t *testing.T, expectation ClaimExpectation) operation.ContextExecState {
	t.Helper()
	req := core.Request{ContextExecID: expectation.Identity.ContextExecID, SessionID: expectation.Identity.SessionID, AuthorityEpoch: expectation.Identity.AuthorityEpoch, Argv: []string{"printf", "ok"}, TimeoutMS: 1000, MaxOutputBytes: 1024}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 3, 0, 0, 0, time.UTC)
	helper := expectation.Helper
	final := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: expectation.Context.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: expectation.Context.CWDObserved, PrivacyState: expectation.Context.PrivacyState}
	return operation.ContextExecState{SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: expectation.Context, Context: &final, BoundaryObservedAt: at, Lifecycle: core.LifecycleHelperAuthenticated, Helper: &helper, CreatedAt: at, UpdatedAt: at}
}

func TestHostPeerVerifierRequiresExactHelperAndStableParentIdentity(t *testing.T) {
	expectedHelper := "/opt/shellbeam/bin/shellbeam"
	ancestorIdentity := "proc_shell_stable"
	facts := map[int]processcore.ProcessFact{
		101: {PID: 101, ParentPID: 42, ExecutableIdentity: expectedHelper, Identity: &processcore.Identity{Value: "proc_helper"}},
		42:  {PID: 42, ParentPID: 1, ExecutableIdentity: "/bin/fish", Identity: &processcore.Identity{Value: ancestorIdentity}},
	}
	verifier := HostPeerVerifier{
		ExpectedHelperExecutable: expectedHelper,
		ParentPID:                42,
		ParentIdentity:           ancestorIdentity,
		PaneTTY:                  "/dev/ttys042",
		CurrentUID:               func() int { return 501 },
		Credentials:              func(net.Conn) (int, uint32, error) { return 101, 501, nil },
		Observe: func(_ context.Context, pid int) (processcore.ProcessFact, error) {
			fact, ok := facts[pid]
			if !ok {
				return processcore.ProcessFact{}, errors.New("missing")
			}
			return fact, nil
		},
		Foreground: func(pid int, tty string) error {
			if pid != 101 || tty != "/dev/ttys042" {
				return errors.New("foreground mismatch")
			}
			return nil
		},
	}
	if err := verifier.Verify(context.Background(), nil, validClaimExpectation(t)); err != nil {
		t.Fatalf("valid peer: %v", err)
	}

	cases := map[string]func(*HostPeerVerifier, map[int]processcore.ProcessFact){
		"uid": func(v *HostPeerVerifier, _ map[int]processcore.ProcessFact) {
			v.Credentials = func(net.Conn) (int, uint32, error) { return 101, 502, nil }
		},
		"helper executable": func(_ *HostPeerVerifier, f map[int]processcore.ProcessFact) {
			x := f[101]
			x.ExecutableIdentity = "/tmp/fake-shellbeam"
			f[101] = x
		},
		"parent missing": func(v *HostPeerVerifier, _ map[int]processcore.ProcessFact) { v.ParentPID = 77 },
		"parent identity": func(v *HostPeerVerifier, _ map[int]processcore.ProcessFact) {
			v.ParentIdentity = "reused_pid_identity"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			copyFacts := map[int]processcore.ProcessFact{}
			for k, v := range facts {
				copyFacts[k] = v
			}
			candidate := verifier
			candidate.Observe = func(_ context.Context, pid int) (processcore.ProcessFact, error) {
				f, ok := copyFacts[pid]
				if !ok {
					return processcore.ProcessFact{}, errors.New("missing")
				}
				return f, nil
			}
			mutate(&candidate, copyFacts)
			if err := candidate.Verify(context.Background(), nil, validClaimExpectation(t)); err == nil {
				t.Fatal("unsafe peer accepted")
			}
		})
	}
}

func TestHostPeerVerifierRejectsWrapperDescendantEvenWithExactShellAncestor(t *testing.T) {
	expectedHelper := "/opt/shellbeam/bin/shellbeam"
	ancestorIdentity := "proc_shell_stable"
	facts := map[int]processcore.ProcessFact{
		101: {PID: 101, ParentPID: 100, ExecutableIdentity: expectedHelper, Identity: &processcore.Identity{Value: "proc_helper"}},
		100: {PID: 100, ParentPID: 42, ExecutableIdentity: "/bin/fish", Identity: &processcore.Identity{Value: "proc_wrapper"}},
		42:  {PID: 42, ParentPID: 1, ExecutableIdentity: "/bin/fish", Identity: &processcore.Identity{Value: ancestorIdentity}},
	}
	verifier := HostPeerVerifier{
		ExpectedHelperExecutable: expectedHelper,
		ParentPID:                42,
		ParentIdentity:           ancestorIdentity,
		PaneTTY:                  "/dev/ttys042",
		CurrentUID:               func() int { return 501 },
		Credentials:              func(net.Conn) (int, uint32, error) { return 101, 501, nil },
		Observe: func(_ context.Context, pid int) (processcore.ProcessFact, error) {
			fact, ok := facts[pid]
			if !ok {
				return processcore.ProcessFact{}, errors.New("missing")
			}
			return fact, nil
		},
		Foreground: func(int, string) error { return nil },
	}
	if err := verifier.Verify(context.Background(), nil, validClaimExpectation(t)); err == nil {
		t.Fatal("wrapper descendant accepted as exact context helper")
	}
}

func TestHostPeerVerifierRejectsExactDirectChildWhenNotForeground(t *testing.T) {
	expectedHelper := "/opt/shellbeam/bin/shellbeam"
	parentIdentity := "proc_shell_stable"
	facts := map[int]processcore.ProcessFact{
		101: {PID: 101, ParentPID: 42, ExecutableIdentity: expectedHelper, Identity: &processcore.Identity{Value: "proc_helper"}},
		42:  {PID: 42, ParentPID: 1, ExecutableIdentity: "/bin/fish", Identity: &processcore.Identity{Value: parentIdentity}},
	}
	verifier := HostPeerVerifier{
		ExpectedHelperExecutable: expectedHelper,
		ParentPID:                42,
		ParentIdentity:           parentIdentity,
		PaneTTY:                  "/dev/ttys042",
		CurrentUID:               func() int { return 501 },
		Credentials:              func(net.Conn) (int, uint32, error) { return 101, 501, nil },
		Observe: func(_ context.Context, pid int) (processcore.ProcessFact, error) {
			fact, ok := facts[pid]
			if !ok {
				return processcore.ProcessFact{}, errors.New("missing")
			}
			return fact, nil
		},
		Foreground: func(pid int, tty string) error {
			if pid != 101 || tty != "/dev/ttys042" {
				t.Fatalf("foreground args pid=%d tty=%q", pid, tty)
			}
			return errors.New("not foreground")
		},
	}
	if err := verifier.Verify(context.Background(), nil, validClaimExpectation(t)); err == nil {
		t.Fatal("non-foreground direct child accepted as context helper")
	}
}

func TestServerAuthRejectsWithStableSafeFailure(t *testing.T) {
	expectation := validClaimExpectation(t)
	server := &Server{Expectation: expectation, VerifyPeer: func(context.Context, net.Conn, ClaimExpectation) error { return errors.New("peer secret diagnostic") }, BindClaim: func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
		t.Fatal("binder reached")
		return operation.ContextExecState{}, nil
	}}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()
	if err := writeFrame(right, HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID}); err != nil {
		t.Fatal(err)
	}
	err := <-done
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Code != failure.ContextHelperAuthFailed {
		t.Fatalf("err=%#v", err)
	}
	if strings.Contains(fmt.Sprint(err), "peer secret diagnostic") {
		t.Fatalf("unsafe cause leaked: %v", err)
	}
}

type oneByteWriter struct{ bytes.Buffer }

func (w *oneByteWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.Buffer.Write(p)
}
func TestClaimCapabilityTransportHandlesShortWritesAndNeverFormatsSecret(t *testing.T) {
	cap, err := NewClaimCapability()
	if err != nil {
		t.Fatal(err)
	}
	hexSecret := hex.EncodeToString(cap.bytes())
	printed := fmt.Sprintf("%v %#v %s", cap, cap, cap)
	if strings.Contains(printed, hexSecret) {
		t.Fatal("formatted capability leaked")
	}
	var w oneByteWriter
	if err := writeCapability(&w, cap); err != nil {
		t.Fatal(err)
	}
	if w.Len() != ClaimCapabilityBytes {
		t.Fatalf("written=%d", w.Len())
	}
	decoded, err := readCapability(&w.Buffer)
	if err != nil || !decoded.equal(cap) {
		t.Fatalf("decoded=%v err=%v", decoded, err)
	}
}

func TestServerRejectsStaleOrMismatchedDurableClaimAfterValidProof(t *testing.T) {
	expectation := validClaimExpectation(t)
	for name, mutate := range map[string]func(*operation.ContextExecState){"session": func(s *operation.ContextExecState) { s.Request.SessionID = "other"; s.Context.SessionID = "other" }, "epoch": func(s *operation.ContextExecState) { s.Request.AuthorityEpoch++; s.Context.AuthorityEpoch++ }, "request": func(s *operation.ContextExecState) {
		s.Request.Argv = []string{"printf", "changed"}
		fp, _ := s.Request.Fingerprint()
		s.RequestFingerprint = fp
		s.Helper.RequestFingerprint = fp
	}} {
		t.Run(name, func(t *testing.T) {
			server := &Server{Expectation: expectation, VerifyPeer: func(context.Context, net.Conn, ClaimExpectation) error { return nil }, BindClaim: func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
				state := validBoundState(t, expectation)
				mutate(&state)
				return state, nil
			}}
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			done := make(chan error, 1)
			go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()
			if err := writeFrame(right, HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID}); err != nil {
				t.Fatal(err)
			}
			challenge, err := readChallengeFrame(right)
			if err != nil {
				t.Fatal(err)
			}
			cap, err := readCapability(right)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := ClaimProof(cap, expectation.Identity, challenge.Challenge)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFrame(right, ProofFrame{ProtocolVersion: ProtocolVersion, Kind: KindProof, Proof: proof}); err != nil {
				t.Fatal(err)
			}
			if err := writeFrame(right, ContextFrame{ProtocolVersion: ProtocolVersion, Kind: KindContext, CWD: expectation.Context.CWDObserved}); err != nil {
				t.Fatal(err)
			}
			err = <-done
			var typed *failure.Failure
			if !errors.As(err, &typed) || typed.Code != failure.ContextHelperAuthFailed {
				t.Fatalf("err=%#v", err)
			}
		})
	}
}

func TestServerRejectsLegacyProtocolBeforePeerProofOrClaimBinding(t *testing.T) {
	expectation := validClaimExpectation(t)
	var peerCalls, bindCalls atomic.Int32
	server := &Server{
		Expectation: expectation,
		VerifyPeer: func(context.Context, net.Conn, ClaimExpectation) error {
			peerCalls.Add(1)
			return nil
		},
		BindClaim: func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
			bindCalls.Add(1)
			return validBoundState(t, expectation), nil
		},
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, err := server.Authenticate(context.Background(), left); done <- err }()

	raw := []byte(`{"protocol_version":2,"kind":"hello","opaque_launch_id":"launch_01"}`)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	if _, err := right.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("legacy v2 helper authenticated against v3 daemon")
	}
	if peerCalls.Load() != 0 || bindCalls.Load() != 0 {
		t.Fatalf("legacy helper reached peer/binder: peer=%d bind=%d", peerCalls.Load(), bindCalls.Load())
	}
}
