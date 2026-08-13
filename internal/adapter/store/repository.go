// Package store implements ShellBeam's file-backed durable authority.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

var ErrNotFound = errors.New("not found")

type Limits struct {
	MaxSessions      int
	MaxSessionOutput int64
	MaxTotalState    int64
	ControlReserve   int64
}
type Repository struct {
	root       string
	limits     Limits
	mu         sync.Mutex
	admit      sync.Mutex
	terminalMu sync.Mutex
	writer     atomicWriter
	locks      map[operation.ID]*sync.Mutex
}

func Open(root string, limits Limits) (*Repository, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("state root must be absolute")
	}
	if limits.MaxSessions < 1 || limits.ControlReserve < 1 {
		return nil, fmt.Errorf("invalid limits")
	}
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("unsafe_state_path")
		}
		if !ownedByCurrent(info) {
			return nil, fmt.Errorf("unsafe state owner")
		}
		if info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("unsafe state permissions")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, p := range []string{root, filepath.Join(root, "operations"), filepath.Join(root, "sessions")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(p, 0700); err != nil {
			return nil, err
		}
	}
	return &Repository{root: root, limits: limits, locks: map[operation.ID]*sync.Mutex{}}, nil
}

func (r *Repository) lock(id operation.ID) func() {
	r.mu.Lock()
	m := r.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		r.locks[id] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func (r *Repository) LoadOperation(_ context.Context, id operation.ID) (operation.Reservation, error) {
	var v operation.Reservation
	return v, readStrict(filepath.Join(r.root, "operations", string(id)+".json"), &v)
}
func (r *Repository) LoadSession(_ context.Context, id operation.SessionID) (session.Snapshot, error) {
	var v session.Snapshot
	return v, readStrict(filepath.Join(r.root, "sessions", string(id), "metadata.json"), &v)
}

func (r *Repository) AppendOutput(_ context.Context, id operation.SessionID, b []byte) (int, app.StoreResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	info, err := os.Stat(path)
	var size int64
	if err == nil {
		size = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if size+int64(len(b)) > r.limits.MaxSessionOutput {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("output_limit_exceeded")}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	n, werr := f.Write(b)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: werr}
	}
	if n != len(b) {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: io.ErrShortWrite}
	}
	if serr != nil {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: serr}
	}
	if cerr != nil {
		return n, app.StoreResult{Durability: app.DurableChange, Err: cerr}
	}
	return n, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) ReadOutput(_ context.Context, id operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if cursor == 0 {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("cursor_out_of_range")
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if cursor < 0 || cursor > info.Size() {
		return nil, info.Size(), fmt.Errorf("cursor_out_of_range")
	}
	if max < 0 {
		return nil, cursor, fmt.Errorf("invalid max")
	}
	b := make([]byte, max)
	n, err := f.ReadAt(b, cursor)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, cursor, err
	}
	return b[:n], cursor + int64(n), nil
}

func readStrict(path string, out any) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing json")
	}
	return nil
}

func (r *Repository) usage() (active int, bytes int64, err error) {
	err = filepath.Walk(r.root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		if filepath.Base(path) == "metadata.json" {
			var s session.Snapshot
			if e := readStrict(path, &s); e != nil {
				return e
			}
			if !s.State.Terminal() {
				active++
			}
		}
		return nil
	})
	return
}

func (r *Repository) Compact(_ context.Context, id operation.SessionID) app.StoreResult {
	snap, err := r.LoadSession(context.Background(), id)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if !snap.State.Terminal() {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("session_not_terminal")}
	}
	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		snap.OutputAvailable = false
		return r.AdvanceSession(context.Background(), snap)
	}
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	snap.OutputBytes = info.Size()
	snap.OutputAvailable = false
	if result := r.AdvanceSession(context.Background(), snap); result.Err != nil {
		return result
	}
	if err := os.Remove(path); err != nil {
		return app.StoreResult{Durability: app.DurableChange, Err: err}
	}
	return app.StoreResult{Durability: app.DurableChange}
}
