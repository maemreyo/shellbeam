package contextexec

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type HostPeerVerifier struct {
	ExpectedHelperExecutable string
	ParentPID                int
	ParentIdentity           string
	PaneTTY                  string
	CurrentUID               func() int
	Credentials              func(net.Conn) (int, uint32, error)
	Observe                  func(context.Context, int) (processcore.ProcessFact, error)
	Foreground               func(int, string) error
}

func (v HostPeerVerifier) Verify(ctx context.Context, conn net.Conn, expectation ClaimExpectation) (core.ShellContinuityProof, error) {
	if !filepath.IsAbs(v.ExpectedHelperExecutable) || v.ParentPID <= 1 || v.ParentIdentity == "" || !filepath.IsAbs(v.PaneTTY) {
		return core.ShellContinuityProof{}, fmt.Errorf("invalid context helper peer expectation")
	}
	creds := v.Credentials
	if creds == nil {
		creds = peerCredentials
	}
	currentUID := v.CurrentUID
	if currentUID == nil {
		currentUID = os.Getuid
	}
	pid, uid, err := creds(conn)
	if err != nil || pid <= 1 || int(uid) != currentUID() {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper peer credentials unproven")
	}
	observe := v.Observe
	if observe == nil {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper process observer missing")
	}
	peer, err := observe(ctx, pid)
	if err != nil {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper peer identity unproven")
	}
	if !sameExecutable(peer.ExecutableIdentity, v.ExpectedHelperExecutable) {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper executable mismatch")
	}
	if peer.ParentPID != v.ParentPID {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper direct parent mismatch")
	}
	parent, err := observe(ctx, peer.ParentPID)
	if err != nil {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper parent unproven")
	}
	if parent.PID != v.ParentPID || parent.Identity == nil || parent.Identity.Value != v.ParentIdentity {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper parent identity mismatch")
	}
	foreground := v.Foreground
	if foreground == nil {
		foreground = platformForegroundVerifier
	}
	if err := foreground(pid, v.PaneTTY); err != nil {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper foreground identity unproven")
	}
	proof := core.ShellContinuityProof{
		SessionID:                expectation.Continuity.SessionID,
		AuthorityEpoch:           expectation.Continuity.AuthorityEpoch,
		ProviderGeneration:       expectation.Continuity.ProviderGeneration,
		ShellRuntimeIdentity:     expectation.Continuity.ShellRuntimeIdentity,
		PaneShellPID:             v.ParentPID,
		PaneShellProcessIdentity: v.ParentIdentity,
		PaneTTY:                  filepath.Clean(v.PaneTTY),
		HelperPID:                pid,
		HelperExecutableIdentity: filepath.Clean(v.ExpectedHelperExecutable),
		ForegroundProven:         true,
		ObservedAt:               time.Now().UTC(),
	}
	if err := proof.ValidateFor(expectation.Continuity); err != nil {
		return core.ShellContinuityProof{}, fmt.Errorf("context helper continuity proof invalid")
	}
	return proof, nil
}

func sameExecutable(observed, expected string) bool {
	if observed == "" || expected == "" {
		return false
	}
	if filepath.Clean(observed) == filepath.Clean(expected) {
		return true
	}
	left, lerr := os.Stat(observed)
	right, rerr := os.Stat(expected)
	return lerr == nil && rerr == nil && os.SameFile(left, right)
}

func VerifyDaemonPeer(ctx context.Context, conn net.Conn, expectedExecutable string, observe func(context.Context, int) (processcore.ProcessFact, error)) error {
	pid, uid, err := peerCredentials(conn)
	if err != nil || pid <= 1 || int(uid) != os.Getuid() {
		return fmt.Errorf("context daemon peer credentials unproven")
	}
	if observe == nil {
		return fmt.Errorf("context daemon process observer missing")
	}
	fact, err := observe(ctx, pid)
	if err != nil || !sameExecutable(fact.ExecutableIdentity, expectedExecutable) {
		return fmt.Errorf("context daemon peer identity unproven")
	}
	return nil
}
