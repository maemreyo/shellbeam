package git

import (
	"context"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SnapshotOptions struct {
	TTL    time.Duration
	Budget time.Duration
	Now    func() time.Time
}

type snapshotCacheEntry struct {
	snapshot core.FastSnapshot
}

type snapshotFlight struct {
	done   chan struct{}
	result core.FastSnapshot
}

type snapshotCache struct {
	mu      sync.Mutex
	entries map[string]snapshotCacheEntry
	flights map[string]*snapshotFlight
}

func newSnapshotCache() *snapshotCache {
	return &snapshotCache{entries: map[string]snapshotCacheEntry{}, flights: map[string]*snapshotFlight{}}
}

func (c *snapshotCache) lookup(key string, now time.Time, ttl time.Duration) (core.FastSnapshot, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return core.FastSnapshot{}, false, false
	}
	age := now.Sub(entry.snapshot.ObservedAt)
	if age < 0 {
		age = 0
	}
	got := entry.snapshot
	got.CacheAgeMS = age.Milliseconds()
	if age <= ttl {
		got.Quality = core.QualityCached
		got.DiagnosticCode = ""
		return got, true, true
	}
	got.Quality = core.QualityStale
	return got, true, false
}

func (c *snapshotCache) begin(key string) (*snapshotFlight, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if flight := c.flights[key]; flight != nil {
		return flight, false
	}
	flight := &snapshotFlight{done: make(chan struct{})}
	c.flights[key] = flight
	return flight, true
}

func (c *snapshotCache) complete(key string, flight *snapshotFlight, result core.FastSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if result.Quality == core.QualityFresh {
		c.entries[key] = snapshotCacheEntry{snapshot: result}
	}
	flight.result = result
	delete(c.flights, key)
	close(flight.done)
}

func waitSnapshotFlight(ctx context.Context, flight *snapshotFlight) (core.FastSnapshot, bool) {
	select {
	case <-flight.done:
		return flight.result, true
	case <-ctx.Done():
		return core.FastSnapshot{}, false
	}
}
