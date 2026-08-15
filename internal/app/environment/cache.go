package environment

import (
	"sync"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
)

type snapshotCacheEntry struct {
	bindingKey string
	snapshot   core.Snapshot
}

type snapshotCache struct {
	mu         sync.Mutex
	maxEntries int
	values     map[string]snapshotCacheEntry
	bindings   map[string]string
}

func newSnapshotCache(maxEntries int) *snapshotCache {
	return &snapshotCache{
		maxEntries: maxEntries,
		values:     make(map[string]snapshotCacheEntry, maxEntries),
		bindings:   make(map[string]string, maxEntries),
	}
}

func (c *snapshotCache) get(key string) (core.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.values[key]
	if !ok {
		return core.Snapshot{}, false
	}
	return cloneSnapshot(entry.snapshot), true
}

func (c *snapshotCache) getByBinding(bindingKey string) (core.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.bindings[bindingKey]
	if !ok {
		return core.Snapshot{}, false
	}
	entry, ok := c.values[key]
	if !ok || entry.bindingKey != bindingKey {
		delete(c.bindings, bindingKey)
		return core.Snapshot{}, false
	}
	return cloneSnapshot(entry.snapshot), true
}

func (c *snapshotCache) put(key, bindingKey string, snapshot core.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.values[key]; ok && existing.bindingKey != "" && existing.bindingKey != bindingKey {
		if c.bindings[existing.bindingKey] == key {
			delete(c.bindings, existing.bindingKey)
		}
	}
	if _, exists := c.values[key]; !exists && len(c.values) >= c.maxEntries {
		c.evictOldestLocked()
	}
	c.values[key] = snapshotCacheEntry{bindingKey: bindingKey, snapshot: cloneSnapshot(snapshot)}
	if bindingKey != "" {
		if previous, ok := c.bindings[bindingKey]; ok && previous != key {
			delete(c.values, previous)
		}
		c.bindings[bindingKey] = key
	}
}

func (c *snapshotCache) evictOldestLocked() {
	var oldestKey string
	for key, entry := range c.values {
		if oldestKey == "" || entry.snapshot.CapturedAt.Before(c.values[oldestKey].snapshot.CapturedAt) ||
			entry.snapshot.CapturedAt.Equal(c.values[oldestKey].snapshot.CapturedAt) && key < oldestKey {
			oldestKey = key
		}
	}
	if oldestKey == "" {
		return
	}
	entry := c.values[oldestKey]
	delete(c.values, oldestKey)
	if entry.bindingKey != "" && c.bindings[entry.bindingKey] == oldestKey {
		delete(c.bindings, entry.bindingKey)
	}
}

func (c *snapshotCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.values)
}

func cloneSnapshot(value core.Snapshot) core.Snapshot {
	value.VariablePresence = append([]core.VariablePresence(nil), value.VariablePresence...)
	value.Toolchains = append([]core.ToolchainObservation(nil), value.Toolchains...)
	value.ToolchainManager = cloneManager(value.ToolchainManager)
	return value
}
