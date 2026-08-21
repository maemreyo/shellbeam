package contextexec

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type HostPeerVerifier struct {
	ExpectedHelperExecutable string
	AncestorPID              int
	AncestorIdentity         string
	CurrentUID               func() int
	Credentials              func(net.Conn) (int, uint32, error)
	Observe                  func(context.Context, int) (processcore.ProcessFact, error)
}

func (v HostPeerVerifier) Verify(ctx context.Context, conn net.Conn, _ ClaimExpectation) error {
	if !filepath.IsAbs(v.ExpectedHelperExecutable) || v.AncestorPID <= 1 || v.AncestorIdentity == "" {
		return fmt.Errorf("invalid context helper peer expectation")
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
		return fmt.Errorf("context helper peer credentials unproven")
	}
	observe := v.Observe
	if observe == nil {
		return fmt.Errorf("context helper process observer missing")
	}
	peer, err := observe(ctx, pid)
	if err != nil {
		return fmt.Errorf("context helper peer identity unproven")
	}
	if !sameExecutable(peer.ExecutableIdentity, v.ExpectedHelperExecutable) {
		return fmt.Errorf("context helper executable mismatch")
	}
	parent := peer.ParentPID
	seen := map[int]struct{}{pid: {}}
	for depth := 0; depth < processcore.MaxTraversalDepth; depth++ {
		if parent <= 1 {
			return fmt.Errorf("context helper ancestor missing")
		}
		if _, ok := seen[parent]; ok {
			return fmt.Errorf("context helper ancestry cycle")
		}
		seen[parent] = struct{}{}
		fact, err := observe(ctx, parent)
		if err != nil {
			return fmt.Errorf("context helper ancestor unproven")
		}
		if fact.PID != parent {
			return fmt.Errorf("context helper ancestor identity mismatch")
		}
		if parent == v.AncestorPID {
			if fact.Identity == nil || fact.Identity.Value != v.AncestorIdentity {
				return fmt.Errorf("context helper ancestor identity mismatch")
			}
			return nil
		}
		parent = fact.ParentPID
	}
	return fmt.Errorf("context helper ancestor depth exceeded")
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
