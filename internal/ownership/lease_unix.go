//go:build linux || darwin

// Package ownership implements daemon lifetime ownership over a directory.
//
// ShellBeam's singleton guarantee used to rest on the socket pathname: startup
// removed a socket it could not reach and bound a new one. A pathname is not an
// ownership token -- anything may unlink it -- and Unix keeps a listener alive
// once its name is gone, so an external `rm daemon.sock` left the old daemon
// serving while a new one bound a fresh socket at the same path. Two daemons
// then shared one state directory, each enforcing its own session limit.
//
// The lock is taken on the directory's own file descriptor, not on a file
// inside it. That distinction is the whole guarantee: a lock on a child file is
// a lock on that file's inode, so unlinking the child lets the next process
// create a different inode at the same name and lock that instead -- the same
// pathname-as-token mistake one level down. A directory cannot be emptied out
// from under its own descriptor, so ownership survives the removal of every
// name inside it.
//
// The kernel releases the lock when the holder dies, so a crashed daemon never
// wedges its successor, and there is no cleanup step a killed process could
// have skipped.
//
// What this does not defend against is replacing the directory itself --
// `rm -rf dir && mkdir dir` yields a new inode that a new owner can lock.
// Defending that needs a lock scoped to a stable parent, which ShellBeam does
// not need today: the runtime and state directories are the identities the
// daemon is configured with, and recreating them is indistinguishable from
// pointing the daemon somewhere new.
package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"golang.org/x/sys/unix"
)

// ErrOwnerAlive reports that a live process already owns the directory.
var ErrOwnerAlive = errors.New("daemon_already_running")

// OwnerFileName holds advisory metadata about the current owner. It is not the
// lock: deleting it does not affect ownership, and its presence does not imply
// the recorded process is alive.
const OwnerFileName = "daemon.owner"

// Owner describes the holder of a lease. It exists so operators and acceptance
// flows can identify which process to stop.
type Owner struct {
	SchemaVersion int       `json:"schema_version"`
	PID           int       `json:"pid"`
	Incarnation   string    `json:"incarnation"`
	AcquiredAt    time.Time `json:"acquired_at"`
}

const ownerSchemaVersion = 1

// Lease is exclusive ownership of a directory, held until Release.
type Lease struct {
	dir string
	// releaseOnce makes Release safe to call concurrently and more than once.
	// Shutdown really does reach it from two goroutines -- a deferred close and
	// a context-cancelled one -- and releasing twice would close a descriptor
	// number that another goroutine may already have been handed.
	releaseOnce sync.Once
	releaseErr  error
	held        *holder
}

// directoryID identifies a directory by what it is rather than by what it is
// called, so two configured paths that resolve to one directory are recognized
// as one thing.
type directoryID struct {
	dev uint64
	ino uint64
}

// holder is the lock itself, which one or more Leases may reference.
//
// A daemon leases both its runtime and its state directory, and those two are
// allowed to be the same directory: nothing ever required them to differ, and
// configurations pointing both at one path used to work. A second exclusive
// flock on a directory this process already holds fails, so such a daemon would
// refuse to start against itself and report daemon_already_running about its
// own lock.
//
// Sharing is therefore explicit -- AcquireWith, given the lease the caller
// already holds -- rather than implicit for anything in the process that names
// the same directory. An unrelated acquirer must still be refused, or the
// guarantee would quietly weaken to "one daemon per process".
type holder struct {
	mu   sync.Mutex
	id   directoryID
	fd   int
	refs int
}

// Acquire takes exclusive ownership of dir, failing immediately with
// ErrOwnerAlive if another live process holds it.
//
// The attempt is non-blocking on purpose: a second daemon must be told it is
// not the owner, not queued behind one that may run for days.
func Acquire(dir, incarnation string) (*Lease, error) {
	if err := prepareDir(dir); err != nil {
		return nil, err
	}
	fd, err := openDirectory(dir)
	if err != nil {
		return nil, err
	}
	id, err := identify(fd)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}

	if err := flockExclusiveNonBlocking(fd); err != nil {
		_ = unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrOwnerAlive
		}
		return nil, err
	}
	lease := &Lease{dir: dir, held: &holder{id: id, fd: fd, refs: 1}}
	if err := writeOwner(dir, incarnation); err != nil {
		lease.Release()
		return nil, err
	}
	return lease, nil
}

// AcquireWith takes ownership of dir, sharing the caller's existing lease when
// that lease already covers the same directory.
//
// This is how a daemon leases both its runtime and its state directory when
// they are one directory. Sharing is deliberately limited to the lease handed
// in: an acquirer that does not already hold the directory is refused exactly
// as it would be by Acquire, so "one daemon per directory" does not soften into
// "one daemon per process".
func AcquireWith(existing *Lease, dir, incarnation string) (*Lease, error) {
	if existing == nil || existing.held == nil {
		return Acquire(dir, incarnation)
	}
	same, err := existing.covers(dir)
	if err != nil || !same {
		return Acquire(dir, incarnation)
	}
	existing.held.mu.Lock()
	defer existing.held.mu.Unlock()
	if existing.held.refs == 0 {
		return Acquire(dir, incarnation)
	}
	existing.held.refs++
	return &Lease{dir: dir, held: existing.held}, nil
}

// covers reports whether this lease's directory is the same directory as dir,
// compared by identity rather than by spelling.
// AcquireRuntimeLease implements the daemon consumer-owned ownership port.
// It deliberately delegates to AcquireWith so state/runtime paths that name
// the same directory share the existing kernel-held ownership reference.
func (l *Lease) AcquireRuntimeLease(dir, incarnation string) (daemonapp.RuntimeLease, error) {
	return AcquireWith(l, dir, incarnation)
}

var _ daemonapp.RuntimeLeaseSource = (*Lease)(nil)

func (l *Lease) covers(dir string) (bool, error) {
	fd, err := openDirectory(dir)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	id, err := identify(fd)
	if err != nil {
		return false, err
	}
	return id == l.held.id, nil
}

func identify(fd int) (directoryID, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return directoryID{}, err
	}
	return directoryID{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}, nil
}

// Held reports whether a live process currently owns the directory. It answers
// by trying to take the lock and releasing it again, so it is the authority a
// startup gate should ask -- not the presence of a socket, a pid file, or the
// owner metadata.
func Held(dir string) (bool, error) {
	fd, err := openDirectory(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer unix.Close(fd)
	if err := flockExclusiveNonBlocking(fd); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
	unlock(fd)
	return false, nil
}

// ReadOwner reports the recorded owner of a directory, whether or not it is
// currently held. Callers must not treat a returned Owner as proof that the
// process is alive; use Held for that.
func ReadOwner(dir string) (Owner, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, OwnerFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Owner{}, false, nil
		}
		return Owner{}, false, err
	}
	var owner Owner
	if len(b) == 0 || json.Unmarshal(b, &owner) != nil || owner.SchemaVersion != ownerSchemaVersion {
		return Owner{}, false, nil
	}
	return owner, true, nil
}

// Release relinquishes ownership. It is safe to call concurrently and more than
// once; every caller observes the same result.
func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	l.releaseOnce.Do(func() {
		if l.held == nil {
			return
		}
		l.held.mu.Lock()
		defer l.held.mu.Unlock()
		if l.held.refs == 0 {
			return
		}
		l.held.refs--
		if l.held.refs > 0 {
			// A shared holder still owns the directory; the lock stays until
			// the last of them leaves.
			return
		}
		unlock(l.held.fd)
		l.releaseErr = unix.Close(l.held.fd)
	})
	return l.releaseErr
}

// Dir reports the directory this lease owns.
func (l *Lease) Dir() string { return l.dir }

func openDirectory(dir string) (int, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	return fd, nil
}

// writeOwner records who took the directory. Only the holder writes it, and it
// carries no authority, so a plain replacing write is enough.
func writeOwner(dir, incarnation string) error {
	b, err := json.Marshal(Owner{
		SchemaVersion: ownerSchemaVersion, PID: os.Getpid(),
		Incarnation: incarnation, AcquiredAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	path := filepath.Join(dir, OwnerFileName)
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_TRUNC|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if _, err = file.Write(append(b, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// prepareDir makes the leased directory exist and be safe to lease. A lease is
// only meaningful if the directory it names cannot be redirected, so the same
// private-ownership rules the runtime and state directories already enforce are
// applied here too.
func prepareDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("lease directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ownedByCurrent(info) || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("unsafe lease directory")
	}
	return nil
}

func flockExclusiveNonBlocking(fd int) error {
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err != unix.EINTR {
			return err
		}
	}
}

func unlock(fd int) {
	for {
		if err := unix.Flock(fd, unix.LOCK_UN); err != unix.EINTR {
			return
		}
	}
}

func ownedByCurrent(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
