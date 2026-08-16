package store

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// admissionLedger is the durable O(1) admission index.
//
// Before this index existed every Start walked the whole state store and
// strict-decoded every sessions/*/metadata.json to recount capacity, so
// admission cost grew with the store's history rather than with the work being
// admitted. The ledger keeps the same two quantities the admission checks need
// and updates them incrementally instead.
//
// ActiveSessions is exact: sessions are created and retired through a single
// metadata choke point (writeSessionMetadata), so every transition across the
// terminal boundary is observed. StateBytes is advisory: it tracks the
// dominant writer (session output) incrementally and is re-derived by a full
// scan at reconciliation and by an off-hot-path refresh, because it guards a
// resource budget rather than a correctness invariant.
type admissionLedger struct {
	SchemaVersion int    `json:"schema_version"`
	Generation    uint64 `json:"generation"`
	// ActiveSessions is always len(ActiveSessionIDs); it is persisted so the
	// index is readable on its own, and validated against the ids on load.
	ActiveSessions   int       `json:"active_sessions"`
	ActiveSessionIDs []string  `json:"active_session_ids"`
	StateBytes       int64     `json:"state_bytes"`
	ReconciledAt     time.Time `json:"reconciled_at"`
}

const (
	admissionSchemaVersion = 1
	admissionFileName      = "admission.json"
	// stateBytesRefreshInterval bounds how stale the advisory byte total may
	// get while the daemon is admitting work.
	stateBytesRefreshInterval = 60 * time.Second
	// admissionPersistInterval coalesces index writes. The in-memory counters
	// are always exact; the persisted copy only seeds a later Open, and daemon
	// startup reconciles from the state store anyway, so paying an fsync per
	// session transition would buy nothing.
	admissionPersistInterval = time.Second
)

func (r *Repository) admissionPath() string { return filepath.Join(r.root, admissionFileName) }

// initAdmissionLedger loads the persisted index. A missing or unreadable index
// is not an error: it leaves the repository unreconciled so that the first
// caller that actually needs the counters pays for one scan. Daemon startup
// reconciles explicitly via AbandonUnresolved.
func (r *Repository) initAdmissionLedger() error {
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	r.activeSessions = map[string]struct{}{}
	r.admission = admissionLedger{SchemaVersion: admissionSchemaVersion}

	// A missing, corrupt, or self-inconsistent index is recoverable by
	// rescanning, so it is discarded rather than trusted. Leaving the
	// repository unreconciled makes the first caller that needs the counters
	// pay for one scan.
	var led admissionLedger
	if err := readStrict(r.admissionPath(), &led); err != nil {
		return nil
	}
	if led.SchemaVersion != admissionSchemaVersion || led.Generation == 0 ||
		led.StateBytes < 0 || led.ActiveSessions != len(led.ActiveSessionIDs) {
		return nil
	}
	for _, id := range led.ActiveSessionIDs {
		r.activeSessions[id] = struct{}{}
	}
	if len(r.activeSessions) != led.ActiveSessions {
		r.activeSessions = map[string]struct{}{}
		return nil
	}
	r.admission = led
	r.stateBytesScannedAt = led.ReconciledAt
	return nil
}

// admissionReconciled reports whether the in-memory counters have been derived
// from a full scan in this process.
func (r *Repository) admissionReconciled() bool {
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	return r.admission.Generation > 0
}

// admissionCounters returns the current counters, reconciling once if the index
// has never been derived from a scan. This is the only place on the admission
// path that may touch the filesystem beyond a single small read.
func (r *Repository) admissionCounters() (active int, bytes int64, err error) {
	if !r.admissionReconciled() {
		if err = r.ReconcileAdmission(); err != nil {
			return 0, 0, err
		}
	}
	r.maybeRefreshStateBytes()
	r.admissionMu.Lock()
	active, bytes = r.admission.ActiveSessions, r.admission.StateBytes
	r.admissionMu.Unlock()
	if !r.nearStateBudget(bytes) {
		return active, bytes, nil
	}
	// The tracked total counts every durable write at full size and no
	// reclamation, so it can only ever run ahead of the store. That keeps
	// admission safe by construction, but next to the budget an overestimate
	// starts rejecting work the store could still hold -- so approaching the
	// budget buys the exact figure with a synchronous re-derive.
	exact, scanErr := r.scanStateBytes()
	if scanErr != nil {
		return 0, 0, scanErr
	}
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	r.admission.StateBytes = exact
	r.stateBytesDelta = 0
	r.stateBytesScannedAt = r.now()
	return r.admission.ActiveSessions, exact, nil
}

// nearStateBudget reports whether the store is close enough to its byte budget
// that a tracked estimate is no longer good enough to admit on.
func (r *Repository) nearStateBudget(bytes int64) bool {
	headroom := r.limits.MaxTotalState - r.limits.ControlReserve
	return headroom <= 0 || bytes >= headroom-headroom/10
}

// ReconcileAdmission re-derives the admission index from a full scan of the
// state store and persists it.
//
// The scan runs without the index lock, so sessions can cross the terminal
// boundary while it is in flight. Those transitions are recorded in an overlay
// and replayed onto the scanned set: each one reflects a durable write that has
// already landed, so it is newer than whatever the scan observed. Without the
// overlay a session that went terminal mid-scan would stay in the set forever,
// leaking a capacity slot that nothing can ever release.
func (r *Repository) ReconcileAdmission() error {
	r.admissionMu.Lock()
	r.reconcileOverlay = map[string]bool{}
	r.admissionMu.Unlock()

	active, bytes, err := r.scanActiveSessions()

	r.admissionMu.Lock()
	overlay := r.reconcileOverlay
	r.reconcileOverlay = nil
	if err != nil {
		r.admissionMu.Unlock()
		return err
	}
	for id, stillActive := range overlay {
		if stillActive {
			active[id] = struct{}{}
		} else {
			delete(active, id)
		}
	}
	r.activeSessions = active
	r.admission.SchemaVersion = admissionSchemaVersion
	r.admission.Generation++
	r.admission.ActiveSessions = len(active)
	r.admission.StateBytes = bytes
	r.admission.ReconciledAt = r.now()
	r.stateBytesScannedAt = r.admission.ReconciledAt
	r.admissionPersistedAt = r.admission.ReconciledAt
	led := r.publishableLedgerLocked()
	r.admissionMu.Unlock()
	// Publication is best effort on purpose. The in-memory counters are what
	// admission reads, and a store that cannot be seeded simply pays for one
	// scan at the next Open -- the index must never be able to fail startup.
	_ = r.persistAdmission(led)
	return nil
}

// publishableLedgerLocked materializes the persisted form of the index from the
// in-memory occupancy set. Ids are sorted so repeated publications of an
// unchanged set produce byte-identical files.
func (r *Repository) publishableLedgerLocked() admissionLedger {
	led := r.admission
	led.ActiveSessions = len(r.activeSessions)
	led.ActiveSessionIDs = make([]string, 0, len(r.activeSessions))
	for id := range r.activeSessions {
		led.ActiveSessionIDs = append(led.ActiveSessionIDs, id)
	}
	sort.Strings(led.ActiveSessionIDs)
	return led
}

// markSessionActive records a session's occupancy of a capacity slot.
//
// Occupancy is tracked as a set rather than a counter on purpose. A counter
// would have to be adjusted by a delta derived from the session's previous
// state, and reading that state, writing the new one, and applying the delta
// cannot be made atomic without serializing every metadata write: two writers
// taking the same session terminal would both observe a live predecessor and
// each release a slot, freeing capacity no session gave up. Set membership is
// idempotent, so any interleaving of repeated writes converges on the truth.
func (r *Repository) markSessionActive(sessionID string, active bool) {
	r.admissionMu.Lock()
	// Recorded before the no-op check: the in-memory set is not authoritative
	// while a reconcile is in flight, so "already agrees" does not imply the
	// scan agrees.
	if r.reconcileOverlay != nil {
		r.reconcileOverlay[sessionID] = active
	}
	_, member := r.activeSessions[sessionID]
	if member == active {
		r.admissionMu.Unlock()
		return
	}
	if active {
		r.activeSessions[sessionID] = struct{}{}
	} else {
		delete(r.activeSessions, sessionID)
	}
	r.admission.ActiveSessions = len(r.activeSessions)
	if r.admission.Generation == 0 {
		// Not reconciled yet: the first admissionCounters call will rescan, so
		// there is nothing durable worth writing.
		r.admissionMu.Unlock()
		return
	}
	r.admission.Generation++
	due := r.now().Sub(r.admissionPersistedAt) >= admissionPersistInterval
	var led admissionLedger
	if due {
		r.admissionPersistedAt = r.now()
		led = r.publishableLedgerLocked()
	}
	r.admissionMu.Unlock()
	if due {
		_ = r.persistAdmission(led)
	}
}

// addStateBytes records durable growth or reclamation. Byte accounting is
// advisory, so a failure to persist is tolerated; the refresh path re-derives
// the total.
func (r *Repository) addStateBytes(delta int64) {
	if delta == 0 {
		return
	}
	r.admissionMu.Lock()
	r.admission.StateBytes += delta
	if r.admission.StateBytes < 0 {
		r.admission.StateBytes = 0
	}
	r.stateBytesDelta += delta
	r.admissionMu.Unlock()
}

// persistAdmission writes the index with a plain atomic writer rather than
// r.writer. The index is derived state that reconciliation can rebuild, so it
// is deliberately outside the durability boundary the reservation and terminal
// protocols are characterized against -- an index write must not consume or
// shift those protocols' persistence checkpoints.
func (r *Repository) persistAdmission(led admissionLedger) error {
	return atomicJSON(r.admissionPath(), led).Err
}

// maybeRefreshStateBytes re-derives the advisory byte total off the hot path.
// The scan itself is stat-only -- it never decodes session metadata -- and runs
// in its own goroutine so admission never pays for it.
func (r *Repository) maybeRefreshStateBytes() {
	r.admissionMu.Lock()
	if r.stateBytesRefreshing || r.admission.Generation == 0 ||
		r.now().Sub(r.stateBytesScannedAt) < stateBytesRefreshInterval {
		r.admissionMu.Unlock()
		return
	}
	r.stateBytesRefreshing = true
	r.stateBytesDelta = 0
	r.admissionMu.Unlock()
	r.stateBytesRefreshWG.Add(1)
	go r.refreshStateBytes()
}

func (r *Repository) refreshStateBytes() {
	defer r.stateBytesRefreshWG.Done()
	scanned, err := r.scanStateBytes()
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	r.stateBytesRefreshing = false
	if err != nil {
		return
	}
	// Growth recorded while the scan was in flight is not visible to the scan.
	r.admission.StateBytes = scanned + r.stateBytesDelta
	r.stateBytesDelta = 0
	r.stateBytesScannedAt = r.now()
}

// awaitStateBytesRefresh blocks until any in-flight refresh completes. Tests
// use it to make the asynchronous path deterministic.
func (r *Repository) awaitStateBytesRefresh() { r.stateBytesRefreshWG.Wait() }

// scanStateBytes sums every regular file under the state root without decoding
// any of them.
func (r *Repository) scanStateBytes() (int64, error) {
	r.fullScans.Add(1)
	var bytes int64
	err := filepath.Walk(r.root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			if transientAtomicWalkError(path, e) {
				return nil
			}
			return e
		}
		if info.Mode().IsRegular() && !isAdmissionIndex(path) {
			bytes += info.Size()
		}
		return nil
	})
	return bytes, err
}

// isAdmissionIndex reports whether a path is the admission index itself.
//
// The index is excluded from byte accounting because reconciliation writes it
// after measuring, so counting it would make every scan disagree with the total
// it had just published by the size of that write.
func isAdmissionIndex(path string) bool { return filepath.Base(path) == admissionFileName }

// writeSessionMetadata is the single choke point for sessions/<id>/metadata.json.
//
// Routing every write through here is what makes ActiveSessions exact: the
// delta is derived from the stored state, so a repeated write of an already
// terminal session cannot double-decrement and a replayed reservation cannot
// double-increment.
func (r *Repository) writeSessionMetadata(sessionID string, v session.Snapshot) app.StoreResult {
	path := filepath.Join(r.root, "sessions", sessionID, "metadata.json")
	result := r.writer.Replace(path, v)
	if result.Err != nil && result.Durability == app.NoDurableChange {
		return result
	}
	r.markSessionActive(sessionID, !v.State.Terminal())
	return result
}
