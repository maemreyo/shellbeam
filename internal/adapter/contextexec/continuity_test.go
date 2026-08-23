package contextexec

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

func claimExpectationWithContinuity(t *testing.T) ClaimExpectation {
	t.Helper()
	expectation := validClaimExpectation(t)
	expectation.Continuity = core.ShellContinuityExpectation{
		SessionID:                expectation.Identity.SessionID,
		AuthorityEpoch:           expectation.Identity.AuthorityEpoch,
		ProviderGeneration:       expectation.Context.ProviderGeneration,
		ShellRuntimeIdentity:     expectation.Context.ShellIdentity,
		PaneShellPID:             42,
		PaneShellProcessIdentity: "proc_shell_stable",
		PaneTTY:                  "/dev/ttys042",
		HelperExecutableIdentity: expectation.Helper.ExecutablePath,
	}
	return expectation
}

func validAdapterContinuityProof(expectation core.ShellContinuityExpectation) core.ShellContinuityProof {
	return core.ShellContinuityProof{
		SessionID:                expectation.SessionID,
		AuthorityEpoch:           expectation.AuthorityEpoch,
		ProviderGeneration:       expectation.ProviderGeneration,
		ShellRuntimeIdentity:     expectation.ShellRuntimeIdentity,
		PaneShellPID:             expectation.PaneShellPID,
		PaneShellProcessIdentity: expectation.PaneShellProcessIdentity,
		PaneTTY:                  expectation.PaneTTY,
		HelperPID:                101,
		HelperExecutableIdentity: expectation.HelperExecutableIdentity,
		ForegroundProven:         true,
		ObservedAt:               time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
	}
}

func TestClaimExpectationRequiresCrossBoundShellContinuity(t *testing.T) {
	valid := claimExpectationWithContinuity(t)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid expectation rejected: %v", err)
	}

	tests := map[string]func(*ClaimExpectation){
		"session":             func(v *ClaimExpectation) { v.Continuity.SessionID = "session_other" },
		"epoch":               func(v *ClaimExpectation) { v.Continuity.AuthorityEpoch++ },
		"provider_generation": func(v *ClaimExpectation) { v.Continuity.ProviderGeneration = "gen_other" },
		"shell_runtime":       func(v *ClaimExpectation) { v.Continuity.ShellRuntimeIdentity = "zsh:runtime_other" },
		"helper_executable":   func(v *ClaimExpectation) { v.Continuity.HelperExecutableIdentity = "/opt/shellbeam/bin/other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			got := valid
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("cross-binding mismatch accepted: %#v", got.Continuity)
			}
		})
	}
}

func TestHostPeerVerifierProducesTypedShellContinuityProof(t *testing.T) {
	expectation := claimExpectationWithContinuity(t)
	expectedHelper := expectation.Helper.ExecutablePath
	facts := map[int]processcore.ProcessFact{
		101: {PID: 101, ParentPID: 42, ExecutableIdentity: expectedHelper, Identity: &processcore.Identity{Value: "proc_helper"}},
		42:  {PID: 42, ParentPID: 1, ExecutableIdentity: "/bin/zsh", Identity: &processcore.Identity{Value: expectation.Continuity.PaneShellProcessIdentity}},
	}
	verifier := HostPeerVerifier{
		ExpectedHelperExecutable: expectedHelper,
		ParentPID:                expectation.Continuity.PaneShellPID,
		ParentIdentity:           expectation.Continuity.PaneShellProcessIdentity,
		PaneTTY:                  expectation.Continuity.PaneTTY,
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
			if pid != 101 || tty != expectation.Continuity.PaneTTY {
				return errors.New("foreground mismatch")
			}
			return nil
		},
	}

	proof, err := verifier.Verify(context.Background(), nil, expectation)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.ValidateFor(expectation.Continuity); err != nil {
		t.Fatalf("proof not bound to expectation: %v", err)
	}
	if proof.HelperPID != 101 || proof.ObservedAt.IsZero() || !proof.ForegroundProven {
		t.Fatalf("proof=%#v", proof)
	}
}

func TestServerForwardsContinuityExpectationAndProofToBinder(t *testing.T) {
	const continuityCanary = "proc_shell_ctx_canary"
	expectation := claimExpectationWithContinuity(t)
	expectation.Continuity.PaneShellProcessIdentity = continuityCanary
	peerProof := validAdapterContinuityProof(expectation.Continuity)
	var boundExpectation core.ShellContinuityExpectation
	var boundProof core.ShellContinuityProof
	var boundState operation.ContextExecState
	server := &Server{
		Expectation: expectation,
		VerifyPeer: func(context.Context, net.Conn, ClaimExpectation) (core.ShellContinuityProof, error) {
			return peerProof, nil
		},
		BindClaim: func(_ context.Context, _ string, _ core.HelperBinding, _ core.ContextBinding,
			continuity core.ShellContinuityExpectation, proof core.ShellContinuityProof,
			_ time.Time, _ string) (operation.ContextExecState, error) {
			boundExpectation, boundProof = continuity, proof
			boundState = validBoundState(t, expectation)
			return boundState, nil
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
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := boundProof.ValidateFor(boundExpectation); err != nil {
		t.Fatalf("binder received unbound continuity: %v", err)
	}
	if boundExpectation != expectation.Continuity || boundProof != peerProof {
		t.Fatalf("bound expectation=%#v proof=%#v", boundExpectation, boundProof)
	}
	for name, value := range map[string]any{"helper_request": request, "durable_state": boundState} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), continuityCanary) {
			t.Fatalf("continuity metadata leaked through %s", name)
		}
	}
}
