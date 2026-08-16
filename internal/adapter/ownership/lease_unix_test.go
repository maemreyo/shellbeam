//go:build linux || darwin

package ownership

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func leaseDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "owned")
	return dir
}

// acquireFromAnotherProcess reports what a separate process sees when it tries
// to take dir.
//
// The refusal these tests are about is between processes: within one process,
// ownership is deliberately shared, because a daemon leases both its runtime
// and its state directory and those may be the same directory.
func acquireFromAnotherProcess(t *testing.T, dir string) string {
	t.Helper()
	probe := exec.Command(os.Args[0], "-test.run=TestAcquireProbeHelper")
	probe.Env = append(os.Environ(), "SHELLBEAM_LEASE_PROBE_DIR="+dir)
	out, err := probe.Output()
	if err != nil {
		t.Fatalf("probe process failed: %v", err)
	}
	// The child is a test binary, so its own framework output shares stdout;
	// the marker is what distinguishes the probe's answer from "PASS".
	for _, line := range strings.Split(string(out), "\n") {
		if verdict, found := strings.CutPrefix(strings.TrimSpace(line), probeMarker); found {
			return verdict
		}
	}
	t.Fatalf("probe process produced no verdict; output:\n%s", out)
	return ""
}

const probeMarker = "lease-probe-verdict:"

// TestAcquireProbeHelper is not a test: it is the child process used by
// acquireFromAnotherProcess.
func TestAcquireProbeHelper(t *testing.T) {
	dir := os.Getenv("SHELLBEAM_LEASE_PROBE_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	lease, err := Acquire(dir, "probe")
	if err != nil {
		fmt.Println(probeMarker + "refused:" + err.Error())
		return
	}
	_ = lease.Release()
	fmt.Println(probeMarker + "acquired")
}

func TestAcquireRefusesAnotherProcess(t *testing.T) {
	dir := leaseDir(t)
	first, err := Acquire(dir, "incarnation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	if got, want := acquireFromAnotherProcess(t, dir), "refused:"+ErrOwnerAlive.Error(); got != want {
		t.Fatalf("another process saw %q, want %q", got, want)
	}
	held, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("Held reported an owned directory as free")
	}
}

func TestAcquireRefusesUnsafeDirectory(t *testing.T) {
	dir := leaseDir(t)
	if err := os.MkdirAll(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "incarnation"); err == nil {
		t.Fatal("acquired a lease in a world-writable directory")
	}
}

func TestHeldIsFalseForUnleasedDirectory(t *testing.T) {
	dir := leaseDir(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	held, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("Held reported an unleased directory as owned")
	}
}

// TestOwnershipIsNotStealableByUnlinking is the finding this package was
// rewritten for. Locking a child file locks that file's inode, so unlinking it
// lets the next caller create a different inode at the same name and lock that
// instead. The lock is on the directory's own descriptor precisely so that
// removing everything inside it changes nothing.
func TestOwnershipIsNotStealableByUnlinking(t *testing.T) {
	dir := leaseDir(t)
	owner, err := Acquire(dir, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := acquireFromAnotherProcess(t, dir), "refused:"+ErrOwnerAlive.Error(); got != want {
		t.Fatalf("another process after emptying the directory saw %q, want %q", got, want)
	}
	held, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("Held reported the directory as free after its contents were removed")
	}
}

// TestSameProcessSharesOneDirectoryLock keeps a daemon from deadlocking against
// itself. Its runtime and state directories may legitimately be the same path,
// and it leases both.
func TestExplicitSharingGivesOneDirectoryLock(t *testing.T) {
	dir := leaseDir(t)
	first, err := Acquire(dir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AcquireWith(first, dir, "daemon")
	if err != nil {
		t.Fatalf("AcquireWith on the lease's own directory = %v, want it to share the lock", err)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	stillHeld, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !stillHeld {
		t.Fatal("releasing one holder dropped a lock another still holds")
	}

	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	free, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if free {
		t.Fatal("ownership outlived its last holder")
	}
}

// TestSharingIsRefusedForAnUnrelatedDirectory keeps the sharing narrow: a lease
// only ever shares with the directory it actually holds.
func TestSharingIsRefusedForAnUnrelatedDirectory(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "mine")
	theirs := filepath.Join(root, "theirs")
	lease, err := Acquire(mine, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	other, err := Acquire(theirs, "other-daemon")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()

	// Holding `mine` must not let this caller borrow its way into `theirs`.
	if got, want := acquireFromAnotherProcess(t, theirs), "refused:"+ErrOwnerAlive.Error(); got != want {
		t.Fatalf("another process saw %q for an unrelated owned directory, want %q", got, want)
	}
	if _, err := AcquireWith(lease, theirs, "daemon"); !errors.Is(err, ErrOwnerAlive) {
		t.Fatalf("AcquireWith on an unrelated owned directory = %v, want %v", err, ErrOwnerAlive)
	}
}

// TestSharingIsByDirectoryIdentityNotPath makes sure the sharing rule follows
// what a directory is rather than what it is called, so two spellings of one
// directory are not mistaken for two directories.
func TestSharingIsByDirectoryIdentityNotPath(t *testing.T) {
	dir := leaseDir(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(dir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	spelledDifferently := filepath.Join(dir, ".", "")
	second, err := AcquireWith(first, spelledDifferently, "daemon")
	if err != nil {
		t.Fatalf("AcquireWith via %q = %v, want it to share with %q", spelledDifferently, err, dir)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	stillHeld, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !stillHeld {
		t.Fatal("a second spelling of the same directory released the original's lock")
	}
}

func TestReadOwnerIdentifiesWhoToStop(t *testing.T) {
	dir := leaseDir(t)
	lease, err := Acquire(dir, "incarnation-a")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	owner, found, err := ReadOwner(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found || owner.PID != os.Getpid() || owner.Incarnation != "incarnation-a" {
		t.Fatalf("owner = %#v, found = %v", owner, found)
	}
}

// TestOwnershipEndsWithTheOwningProcess is the guarantee that keeps a lease
// from outliving a crash: the kernel drops the lock when the holder dies, with
// no cleanup step that a killed process could have skipped.
func TestOwnershipEndsWithTheOwningProcess(t *testing.T) {
	dir := leaseDir(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(os.Args[0], "-test.run=TestLeaseHolderHelper")
	helper.Env = append(os.Environ(), "SHELLBEAM_LEASE_HELPER_DIR="+dir)
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	// The helper prints one byte once it holds the lease.
	ack := make([]byte, 1)
	if _, err := stdout.Read(ack); err != nil {
		t.Fatal(err)
	}
	held, err := Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("helper process did not take the lease")
	}

	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()

	held, err = Held(dir)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("lease outlived the process that held it")
	}
	successor, err := Acquire(dir, "successor")
	if err != nil {
		t.Fatalf("successor could not take the directory: %v", err)
	}
	_ = successor.Release()
}

// TestLeaseHolderHelper is not a test: it is the child process for
// TestOwnershipEndsWithTheOwningProcess, which kills it while it holds a lease.
func TestLeaseHolderHelper(t *testing.T) {
	dir := os.Getenv("SHELLBEAM_LEASE_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	lease, err := Acquire(dir, "helper")
	if err != nil {
		t.Fatal(err)
	}
	// Holding the reference matters: discarding it would let the runtime
	// finalize the descriptor and drop the lock without the process dying,
	// which is the very thing the parent is trying to prove only happens on
	// death. The parent SIGKILLs this process, so the deferred release never
	// runs -- the kernel is what frees the lock.
	defer lease.Release()
	_, _ = os.Stdout.Write([]byte{'.'})
	select {} // killed by the parent
}
