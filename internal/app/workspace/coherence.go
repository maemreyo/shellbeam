package workspace

import (
	"strings"
	"sync"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

// CoherenceTracker records ShellBeam-owned shell lifecycle transitions for
// state-root cache invalidation. It is intentionally non-durable.
type CoherenceTracker struct {
	mu                sync.Mutex
	daemonIncarnation string
	epoch             uint64
	active            int
}

func NewCoherenceTracker(daemonIncarnation string) *CoherenceTracker {
	return &CoherenceTracker{daemonIncarnation: strings.TrimSpace(daemonIncarnation)}
}

func (t *CoherenceTracker) BeginManagedShell() *ManagedShellLease {
	t.mu.Lock()
	t.epoch++
	t.active++
	t.mu.Unlock()
	return &ManagedShellLease{tracker: t}
}

func (t *CoherenceTracker) Invalidate(reason string) {
	_ = strings.TrimSpace(reason)
	t.mu.Lock()
	t.epoch++
	t.mu.Unlock()
}

func (t *CoherenceTracker) CaptureBarrier() core.CoherenceBarrier {
	t.mu.Lock()
	defer t.mu.Unlock()
	return core.CoherenceBarrier{
		DaemonIncarnation:            t.daemonIncarnation,
		Epoch:                        t.epoch,
		ActiveManagedShellOperations: t.active,
	}
}

type ManagedShellLease struct {
	tracker *CoherenceTracker
	once    sync.Once
}

func (l *ManagedShellLease) End() {
	if l == nil || l.tracker == nil {
		return
	}
	l.once.Do(func() { l.tracker.endManagedShell() })
}

func (t *CoherenceTracker) endManagedShell() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active > 0 {
		t.active--
	}
	t.epoch++
}
