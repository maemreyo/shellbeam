//go:build linux || darwin

package ipc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/adapter/ownership"
)

func runtimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shellbeam-split-brain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func listenOrSkip(t *testing.T, runtime string) *Server {
	t.Helper()
	srv, err := Listen(runtime, fakeActions{})
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	return srv
}

// TestUnlinkedSocketDoesNotYieldOwnership is the split-brain regression.
//
// This is exactly what the manual acceptance gate did: it removed the socket
// pathname before starting a daemon. Unix keeps the old listener alive once its
// name is gone, so the old daemon kept serving while a new one bound a fresh
// socket at the same path, and two daemons shared one runtime and state
// directory -- each enforcing its own session limit.
//
// Ownership must survive losing the pathname.
func TestUnlinkedSocketDoesNotYieldOwnership(t *testing.T) {
	runtime := runtimeDir(t)
	owner := listenOrSkip(t, runtime)
	defer owner.Close()
	go func() { _ = owner.Serve() }()

	// An external actor removes the socket, exactly as `rm -f daemon.sock` did.
	if err := os.Remove(filepath.Join(runtime, "daemon.sock")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(runtime, "daemon.sock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still present: %v", err)
	}

	second, err := Listen(runtime, fakeActions{})
	if err == nil {
		second.Close()
		t.Fatal("a second daemon started while the first still owned the runtime directory")
	}
	if !errors.Is(err, ownership.ErrOwnerAlive) {
		t.Fatalf("second daemon failed with %v, want %v", err, ownership.ErrOwnerAlive)
	}
}

// TestOwnershipReleasedOnShutdownAllowsSuccessor keeps the guarantee from
// becoming a wedge: a daemon that stops must hand the directory over.
func TestOwnershipReleasedOnShutdownAllowsSuccessor(t *testing.T) {
	runtime := runtimeDir(t)
	first := listenOrSkip(t, runtime)
	go func() { _ = first.Serve() }()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	held, err := ownership.Held(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("runtime directory still owned after shutdown")
	}
	second := listenOrSkip(t, runtime)
	defer second.Close()
}

// TestOwnershipIsHeldForTheWholeServingLife guards against a regression to the
// old shape, where the lock was released as soon as startup finished.
func TestOwnershipIsHeldForTheWholeServingLife(t *testing.T) {
	runtime := runtimeDir(t)
	srv := listenOrSkip(t, runtime)
	defer srv.Close()
	go func() { _ = srv.Serve() }()

	held, err := ownership.Held(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("runtime directory unowned while its daemon is serving")
	}
	recorded, found, err := ownership.ReadOwner(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !found || recorded.PID != os.Getpid() {
		t.Fatalf("recorded owner = %#v, found = %v", recorded, found)
	}
}

// TestOwnershipSurvivesEmptyingTheRuntimeDirectory is the strong form of the
// guarantee. An earlier version of this test removed only the owner metadata
// and passed for the wrong reason: the surviving socket, not the lease, was
// what stopped the second daemon. Every pathname the directory contains is
// removed here, so nothing but the directory's own identity can be doing the
// work -- which is exactly the claim, since a lock on any child file would be a
// lock on that child's inode and could be stepped around by recreating it.
func TestOwnershipSurvivesEmptyingTheRuntimeDirectory(t *testing.T) {
	runtime := runtimeDir(t)
	srv := listenOrSkip(t, runtime)
	defer srv.Close()
	go func() { _ = srv.Serve() }()

	entries, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nothing to remove; the runtime directory should hold at least a socket")
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(runtime, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	remaining, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("runtime directory still holds %d entries", len(remaining))
	}

	second, err := Listen(runtime, fakeActions{})
	if err == nil {
		second.Close()
		t.Fatal("a second daemon started after every pathname in the runtime directory was removed")
	}
	if !errors.Is(err, ownership.ErrOwnerAlive) {
		t.Fatalf("second daemon failed with %v, want %v", err, ownership.ErrOwnerAlive)
	}
}

// TestOwnershipSurvivesOwnerMetadataUnlink keeps the advisory file advisory.
func TestOwnershipSurvivesOwnerMetadataUnlink(t *testing.T) {
	runtime := runtimeDir(t)
	srv := listenOrSkip(t, runtime)
	defer srv.Close()
	go func() { _ = srv.Serve() }()

	if err := os.Remove(filepath.Join(runtime, ownership.OwnerFileName)); err != nil {
		t.Fatal(err)
	}
	held, err := ownership.Held(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("ownership was lost with the owner metadata file")
	}
}

// TestServerSharesOwnershipWithItsOwnStateLease covers the configuration where
// the runtime and state directories are the same path.
//
// Nothing ever required them to differ, and a daemon leases both. Ownership is
// shared by reference within a process precisely so that such a daemon does not
// refuse to start against its own lock, and the server must not drop that lock
// out from under the state lease when it closes.
func TestServerSharesOwnershipWithItsOwnStateLease(t *testing.T) {
	dir := runtimeDir(t)
	stateLease, err := ownership.Acquire(dir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	defer stateLease.Release()

	// Without being handed the state lease the server would be refused by its
	// own daemon's lock, which is the compatibility regression this covers.
	if _, err := Listen(dir, fakeActions{}); !errors.Is(err, ownership.ErrOwnerAlive) {
		t.Fatalf("listening without the state lease = %v, want %v", err, ownership.ErrOwnerAlive)
	}

	srv, err := ListenPendingAs(dir, "daemon", fakeActions{}, stateLease)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	srv.MarkReady()
	go func() { _ = srv.Serve() }()
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	stillHeld, err := ownership.Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !stillHeld {
		t.Fatal("closing the server released ownership the state lease still holds")
	}
	if err := stateLease.Release(); err != nil {
		t.Fatal(err)
	}
	free, err := ownership.Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if free {
		t.Fatal("ownership outlived its last holder")
	}
}
