//go:build darwin

package contextexec

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
)

func TestDarwinPeerCredentialsExposeExactPeerPIDAndUIDOnUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sb-cx-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	type result struct {
		pid int
		uid uint32
		err error
	}
	accepted := make(chan result, 1)
	release := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			accepted <- result{err: err}
			return
		}
		defer conn.Close()
		pid, uid, err := peerCredentials(conn)
		accepted <- result{pid: pid, uid: uid, err: err}
		<-release
	}()

	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer close(release)

	select {
	case got := <-accepted:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.pid != os.Getpid() {
			t.Fatalf("peer pid=%d want=%d", got.pid, os.Getpid())
		}
		if got.uid != uint32(os.Getuid()) {
			t.Fatalf("peer uid=%d want=%d", got.uid, os.Getuid())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer credential probe hung")
	}

	clientPID, clientUID, err := peerCredentials(client)
	if err != nil {
		t.Fatal(err)
	}
	if clientPID != os.Getpid() || clientUID != uint32(os.Getuid()) {
		t.Fatalf("reverse peer pid=%d uid=%d want pid=%d uid=%d", clientPID, clientUID, os.Getpid(), os.Getuid())
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyDaemonPeer(context.Background(), client, executable, processadapter.NewHostInspector().Observe); err != nil {
		t.Fatalf("daemon peer verification failed: %v", err)
	}
}
