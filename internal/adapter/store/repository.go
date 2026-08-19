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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

var ErrNotFound = errors.New("not found")

type Limits struct {
	MaxSessions                   int
	MaxSessionOutput              int64
	MaxTotalState                 int64
	ControlReserve                int64
	MaxTelemetrySamples           int
	MaxTelemetryBytes             int64
	MaxTelemetryKeys              int
	MaxTelemetryKeysPerRepository int
	MaxTelemetrySamplesPerKey     int
	MaxTelemetryAge               time.Duration
	MaxReproCapsules              int
	MaxReproBytes                 int64
	MaxReproAge                   time.Duration
	MaxMutationScopesPerActivity  int
	MaxMutationScopesPerWorkspace int
}
type Repository struct {
	root                     string
	limits                   Limits
	mu                       sync.Mutex
	admit                    sync.Mutex
	terminalMu               sync.Mutex
	observationMu            sync.Mutex
	observationVisibilityMu  sync.RWMutex
	eventMu                  sync.Mutex
	structuredMu             sync.Mutex
	blobBudgetMu             sync.Mutex
	telemetryMu              sync.Mutex
	inputTraceMu             sync.Mutex
	reproMu                  sync.Mutex
	checkpointMu             sync.Mutex
	mutationScopeMu          sync.Mutex
	persistentSessionMu      sync.Mutex
	evidenceMu               sync.Mutex
	evidenceValidityMu       sync.Mutex
	verificationMu           sync.Mutex
	observationHighWatermark uint64
	observationWake          chan struct{}
	writer                   atomicWriter
	locks                    map[operation.ID]*sync.Mutex
	now                      func() time.Time

	admissionMu sync.Mutex
	admission   admissionLedger
	// activeSessions holds the ids currently occupying a capacity slot. It is
	// bounded by live sessions, not by history.
	activeSessions map[string]struct{}
	// reconcileOverlay is non-nil only while a reconciliation scan is in
	// flight, capturing transitions the scan cannot have observed.
	reconcileOverlay     map[string]bool
	admissionPersistedAt time.Time
	stateBytesScannedAt  time.Time
	stateBytesDelta      int64
	stateBytesRefreshing bool
	stateBytesRefreshWG  sync.WaitGroup
	// fullScans counts full-tree scans of the state store. Admission must not
	// increment it; the regression test asserts that.
	fullScans        atomic.Uint64
	blobReservations map[string]int64
}

const (
	defaultMaxTelemetrySamples                 = 2048
	defaultMaxTelemetryBytes             int64 = 16 << 20
	defaultMaxTelemetryKeys                    = 512
	defaultMaxTelemetryKeysPerRepository       = 128
	defaultMaxTelemetrySamplesPerKey           = 64
	defaultMaxReproCapsules                    = 256
	defaultMaxReproBytes                 int64 = 16 << 20
)

const (
	defaultMaxTelemetryAge               = 30 * 24 * time.Hour
	defaultMaxReproAge                   = 30 * 24 * time.Hour
	defaultMaxMutationScopesPerActivity  = 16
	defaultMaxMutationScopesPerWorkspace = 64
)

func normalizeTelemetryLimits(limits Limits) Limits {
	if limits.MaxTelemetrySamples == 0 {
		limits.MaxTelemetrySamples = defaultMaxTelemetrySamples
	}
	if limits.MaxTelemetryBytes == 0 {
		limits.MaxTelemetryBytes = defaultMaxTelemetryBytes
	}
	if limits.MaxTelemetryKeys == 0 {
		limits.MaxTelemetryKeys = defaultMaxTelemetryKeys
	}
	if limits.MaxTelemetryKeysPerRepository == 0 {
		limits.MaxTelemetryKeysPerRepository = defaultMaxTelemetryKeysPerRepository
	}
	if limits.MaxTelemetrySamplesPerKey == 0 {
		limits.MaxTelemetrySamplesPerKey = defaultMaxTelemetrySamplesPerKey
	}
	if limits.MaxTelemetryAge == 0 {
		limits.MaxTelemetryAge = defaultMaxTelemetryAge
	}
	if limits.MaxReproCapsules == 0 {
		limits.MaxReproCapsules = defaultMaxReproCapsules
	}
	if limits.MaxReproBytes == 0 {
		limits.MaxReproBytes = defaultMaxReproBytes
	}
	if limits.MaxReproAge == 0 {
		limits.MaxReproAge = defaultMaxReproAge
	}
	if limits.MaxMutationScopesPerActivity == 0 {
		limits.MaxMutationScopesPerActivity = defaultMaxMutationScopesPerActivity
	}
	if limits.MaxMutationScopesPerWorkspace == 0 {
		limits.MaxMutationScopesPerWorkspace = defaultMaxMutationScopesPerWorkspace
	}
	return limits
}

func Open(root string, limits Limits) (*Repository, error) {
	limits = normalizeTelemetryLimits(limits)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("state root must be absolute")
	}
	if limits.MaxSessions < 1 || limits.ControlReserve < 1 || limits.MaxTelemetrySamples < 1 || limits.MaxTelemetryBytes < 1 || limits.MaxTelemetryKeys < 1 || limits.MaxTelemetryKeysPerRepository < 1 || limits.MaxTelemetrySamplesPerKey < 1 || limits.MaxTelemetryAge < 0 || limits.MaxReproCapsules < 1 || limits.MaxReproBytes < 1 || limits.MaxReproAge < 0 || limits.MaxMutationScopesPerActivity < 1 || limits.MaxMutationScopesPerWorkspace < limits.MaxMutationScopesPerActivity {
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
	for _, p := range []string{
		root, filepath.Join(root, "operations"), filepath.Join(root, "typed-intents"), filepath.Join(root, "sessions"),
		filepath.Join(root, "repositories"), filepath.Join(root, "workspaces"), filepath.Join(root, "activities"),
	} {
		if err := os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(p, 0700); err != nil {
			return nil, err
		}
	}
	repository := &Repository{root: root, limits: limits, locks: map[operation.ID]*sync.Mutex{}, observationWake: make(chan struct{}, 1), now: func() time.Time { return time.Now().UTC() }, blobReservations: map[string]int64{}}
	repository.writer = atomicWriter{onBytes: repository.addStateBytes}
	if err := repository.initObservationStore(); err != nil {
		return nil, err
	}
	if err := repository.initEventStore(); err != nil {
		return nil, err
	}
	if err := repository.initStructuredResultStore(); err != nil {
		return nil, err
	}
	if err := repository.RecoverStructuredArtifacts(context.Background()); err != nil {
		return nil, err
	}
	if err := repository.initTelemetryStore(); err != nil {
		return nil, err
	}
	if err := repository.initInputTraceStore(); err != nil {
		return nil, err
	}
	if err := repository.initReproStore(); err != nil {
		return nil, err
	}
	if err := repository.initEvidenceStore(); err != nil {
		return nil, err
	}
	if err := repository.initMutationScopeStore(); err != nil {
		return nil, err
	}
	if err := repository.initPersistentSessionStore(); err != nil {
		return nil, err
	}
	if err := repository.initVerificationStore(); err != nil {
		return nil, err
	}
	if err := repository.initAdmissionLedger(); err != nil {
		return nil, err
	}
	return repository, nil
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

func (r *Repository) FindOperation(ctx context.Context, id operation.ID) (operation.Reservation, bool, error) {
	v, err := r.LoadOperation(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return operation.Reservation{}, false, nil
	}
	if err != nil {
		return operation.Reservation{}, false, err
	}
	return v, true, nil
}
func (r *Repository) LoadSession(_ context.Context, id operation.SessionID) (session.Snapshot, error) {
	var v session.Snapshot
	return v, readStrict(filepath.Join(r.root, "sessions", string(id), "metadata.json"), &v)
}

func (r *Repository) AppendOutput(ctx context.Context, id operation.SessionID, b []byte) (int, app.StoreResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	start, err := outputSize(path)
	if err != nil {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if start+int64(len(b)) > r.limits.MaxSessionOutput {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("output_limit_exceeded")}
	}
	seq, prepared := r.prepareOutputObservation(ctx, id, start, start+int64(len(b)))
	if prepared.Err != nil {
		return 0, prepared
	}
	n, result := appendOutputBytes(path, b)
	r.finishOutputObservation(seq, path, start, start+int64(len(b)), result)
	result.ObservationSeq = uint64(seq)
	// Session output is the dominant source of state growth, so it is the one
	// writer the advisory byte total tracks incrementally.
	r.addStateBytes(int64(n))
	return n, result
}

func (r *Repository) OutputExtent(ctx context.Context, id operation.SessionID) (outputview.Extent, error) {
	extent := outputview.Extent{SessionID: string(id), State: outputview.RetentionUnavailable}
	snap, err := r.LoadSession(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return extent, nil
	}
	if err != nil {
		return outputview.Extent{}, err
	}
	extent.Bytes = snap.OutputBytes
	extent.Terminal = snap.State.Terminal()
	if !snap.OutputAvailable {
		extent.State = outputview.RetentionCompacted
		return extent, nil
	}
	path := filepath.Join(r.root, "sessions", string(id), "output.log")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if snap.OutputBytes == 0 {
			extent.State = outputview.RetentionRetained
		}
		return extent, nil
	}
	if err != nil {
		return outputview.Extent{}, err
	}
	extent.State = outputview.RetentionRetained
	extent.Bytes = info.Size()
	return extent, nil
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

// scanActiveSessions derives the admission index from a full scan of the state
// store: which sessions still occupy a capacity slot, and how many bytes the
// store holds.
//
// It costs O(history) -- a stat of every file plus a strict decode of every
// session metadata document -- so it belongs to reconciliation only, never to
// the admission path. See admission.go.
func transientAtomicWalkError(root, path string, err error) bool {
	return errors.Is(err, os.ErrNotExist) && path != root
}

func (r *Repository) scanActiveSessions() (map[string]struct{}, int64, error) {
	r.fullScans.Add(1)
	active := map[string]struct{}{}
	var bytes int64
	err := filepath.Walk(r.root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			if transientAtomicWalkError(r.root, path, e) {
				return nil
			}
			return e
		}
		if info.Mode().IsRegular() && !isAdmissionIndex(path) {
			bytes += info.Size()
		}
		if filepath.Base(path) == "metadata.json" {
			if strings.HasPrefix(filepath.Base(filepath.Dir(path)), gcStagingPrefix) {
				// Withdrawn from view by retention; it counts for nothing.
				return nil
			}
			var s session.Snapshot
			if e := readStrict(path, &s); e != nil {
				return e
			}
			if !s.State.Terminal() {
				active[filepath.Base(filepath.Dir(path))] = struct{}{}
			}
		}
		return nil
	})
	return active, bytes, err
}

// usage reports the same quantities as counts, for callers that only compare
// totals.
func (r *Repository) usage() (int, int64, error) {
	active, bytes, err := r.scanActiveSessions()
	return len(active), bytes, err
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
	r.addStateBytes(-info.Size())
	return app.StoreResult{Durability: app.DurableChange}
}
