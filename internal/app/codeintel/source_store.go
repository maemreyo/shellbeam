package codeintel

import (
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

type SourceStoreConfig struct {
	MaxEntries       int
	MaxRetainedBytes int64
	TTL              time.Duration
	MaxTombstones    int
	Now              func() time.Time
	NextID           func() core.SourceRefID
}

type SourceStoreStats struct {
	Entries       int
	RetainedBytes int64
	Tombstones    int
}

type sourceEntry struct {
	bound     BoundSource
	expiresAt time.Time
}

type SourceStore struct {
	mu sync.Mutex

	maxEntries       int
	maxRetainedBytes int64
	ttl              time.Duration
	maxTombstones    int
	now              func() time.Time
	nextID           func() core.SourceRefID

	entries        map[core.SourceRefID]sourceEntry
	order          []core.SourceRefID
	retainedBytes  int64
	tombstones     map[core.SourceRefID]time.Time
	tombstoneOrder []core.SourceRefID
}

func NewSourceStore(config SourceStoreConfig) (*SourceStore, error) {
	if config.MaxEntries < 1 || config.MaxRetainedBytes < 1 || config.TTL <= 0 || config.MaxTombstones < 0 {
		return nil, fmt.Errorf("invalid source store config")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NextID == nil {
		config.NextID = func() core.SourceRefID {
			return core.SourceRefID("src_" + ulid.Make().String())
		}
	}
	return &SourceStore{
		maxEntries:       config.MaxEntries,
		maxRetainedBytes: config.MaxRetainedBytes,
		ttl:              config.TTL,
		maxTombstones:    config.MaxTombstones,
		now:              config.Now,
		nextID:           config.NextID,
		entries:          make(map[core.SourceRefID]sourceEntry),
		tombstones:       make(map[core.SourceRefID]time.Time),
	}, nil
}

func (s *SourceStore) Retain(ref core.SourceRef, sourceBytes []byte) (BoundSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	if int64(len(sourceBytes)) > s.maxRetainedBytes {
		return BoundSource{}, newError(CodeQueryBudgetExceeded, false, fmt.Errorf("source byte budget exceeded"))
	}
	id, err := s.nextUniqueIDLocked()
	if err != nil {
		return BoundSource{}, err
	}
	ref.ID = id
	if err := ref.Validate(); err != nil {
		return BoundSource{}, fmt.Errorf("invalid source ref: %w", err)
	}

	for len(s.entries) >= s.maxEntries || s.retainedBytes+int64(len(sourceBytes)) > s.maxRetainedBytes {
		if !s.evictOldestLocked() {
			return BoundSource{}, newError(CodeQueryBudgetExceeded, false, fmt.Errorf("source store capacity unavailable"))
		}
	}
	stored := BoundSource{Ref: ref, Bytes: cloneBytes(sourceBytes)}
	s.entries[id] = sourceEntry{bound: stored, expiresAt: s.now().Add(s.ttl)}
	s.order = append(s.order, id)
	s.retainedBytes += int64(len(sourceBytes))
	return cloneBoundSource(stored), nil
}

func (s *SourceStore) Resolve(id core.SourceRefID) (BoundSource, SourceRefState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	if entry, ok := s.entries[id]; ok {
		return cloneBoundSource(entry.bound), SourceRefCurrent
	}
	if _, ok := s.tombstones[id]; ok {
		return BoundSource{}, SourceRefExpired
	}
	return BoundSource{}, SourceRefUnavailable
}

func (s *SourceStore) Stats() SourceStoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(s.now())
	return SourceStoreStats{
		Entries:       len(s.entries),
		RetainedBytes: s.retainedBytes,
		Tombstones:    len(s.tombstones),
	}
}

func (s *SourceStore) nextUniqueIDLocked() (core.SourceRefID, error) {
	for range 32 {
		id := s.nextID()
		if _, err := core.ParseSourceRefID(string(id)); err != nil {
			return "", fmt.Errorf("source id generator returned invalid id: %w", err)
		}
		if _, active := s.entries[id]; active {
			continue
		}
		if _, expired := s.tombstones[id]; expired {
			continue
		}
		return id, nil
	}
	return "", fmt.Errorf("source id generator collision budget exceeded")
}

func (s *SourceStore) expireLocked(now time.Time) {
	if len(s.order) == 0 {
		return
	}
	kept := s.order[:0]
	for _, id := range s.order {
		entry, ok := s.entries[id]
		if !ok {
			continue
		}
		if now.Before(entry.expiresAt) {
			kept = append(kept, id)
			continue
		}
		delete(s.entries, id)
		s.retainedBytes -= int64(len(entry.bound.Bytes))
		s.addTombstoneLocked(id, now)
	}
	s.order = append([]core.SourceRefID(nil), kept...)
}

func (s *SourceStore) addTombstoneLocked(id core.SourceRefID, expiredAt time.Time) {
	if s.maxTombstones == 0 {
		return
	}
	if _, exists := s.tombstones[id]; exists {
		return
	}
	s.tombstones[id] = expiredAt
	s.tombstoneOrder = append(s.tombstoneOrder, id)
	for len(s.tombstoneOrder) > s.maxTombstones {
		oldest := s.tombstoneOrder[0]
		s.tombstoneOrder = s.tombstoneOrder[1:]
		delete(s.tombstones, oldest)
	}
}

func (s *SourceStore) evictOldestLocked() bool {
	for len(s.order) > 0 {
		id := s.order[0]
		s.order = s.order[1:]
		entry, ok := s.entries[id]
		if !ok {
			continue
		}
		delete(s.entries, id)
		s.retainedBytes -= int64(len(entry.bound.Bytes))
		return true
	}
	return false
}

func cloneBoundSource(source BoundSource) BoundSource {
	return BoundSource{Ref: source.Ref, Bytes: cloneBytes(source.Bytes)}
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}
	return append([]byte(nil), data...)
}
