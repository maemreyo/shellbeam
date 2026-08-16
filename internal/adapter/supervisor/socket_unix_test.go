//go:build linux || darwin

package supervisor

import (
	"net"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListenControlPublishesUserOnlySocketAndAcceptsCurrentUserPeer(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("/tmp", "sb-sup-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	layout, err := PreparePrivateState(filepath.Join(base, "runtime"), "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := ListenControl(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Lstat(layout.SocketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("socket info=%v err=%v", info, err)
	}
	dialed := make(chan error, 1)
	go func() {
		conn, err := net.DialTimeout("unix", layout.SocketPath, time.Second)
		if err == nil {
			_ = conn.Close()
		}
		dialed <- err
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if uid, err := peerUID(conn); err != nil || int(uid) != os.Getuid() {
		_ = conn.Close()
		t.Fatalf("peer uid=%d err=%v", uid, err)
	}
	_ = conn.Close()
	if err := <-dialed; err != nil {
		t.Fatal(err)
	}
}

func TestListenControlRejectsSocketPathReplacement(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("/tmp", "sb-sup-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	layout, err := PreparePrivateState(filepath.Join(base, "runtime"), "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("not-a-socket"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, layout.SocketPath); err != nil {
		t.Fatal(err)
	}
	if listener, err := ListenControl(layout); err == nil {
		_ = listener.Close()
		t.Fatal("socket symlink replacement accepted")
	} else {
		public := failure.Public(err)
		if public.Code != failure.SupervisorUnavailable || strings.Contains(public.Message, layout.SocketPath) {
			t.Fatalf("unsafe socket error projection: %#v", public)
		}
	}
}

func TestStagedControlSocketPathNeverExceedsPublishedPathLength(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "shellbeam-b1-native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(filepath.Join(base, "run"), "01M049SOCKETLENGTH000000000", "generation-socket-length", capability)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := stagedControlSocketPath(layout.SessionDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) > len(layout.SocketPath) {
		t.Fatalf("staged socket path=%d exceeds published=%d: staged=%q published=%q", len(staged), len(layout.SocketPath), staged, layout.SocketPath)
	}
}
