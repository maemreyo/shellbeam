package codeintel

import (
	"bytes"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestSourceStoreRetainsImmutableBytesAndOpaqueIDs(t *testing.T) {
	clock := newTestClock()
	store := newTestSourceStore(t, clock, 2, 64, time.Minute, 2)
	input := []byte("alpha")
	first, err := store.Retain(testSourceRef(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ParseSourceRefID(string(first.Ref.ID)); err != nil {
		t.Fatalf("invalid opaque source id: %v", err)
	}

	input[0] = 'X'
	first.Bytes[1] = 'Y'
	resolved, state := store.Resolve(first.Ref.ID)
	if state != SourceRefCurrent || string(resolved.Bytes) != "alpha" {
		t.Fatalf("stored bytes rebound/mutated: state=%q bytes=%q", state, resolved.Bytes)
	}

	second, err := store.Retain(testSourceRef(), []byte("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Ref.ID == first.Ref.ID {
		t.Fatal("source id was recycled for a different byte representation")
	}
	resolved, state = store.Resolve(first.Ref.ID)
	if state != SourceRefCurrent || string(resolved.Bytes) != "alpha" {
		t.Fatalf("first source ref changed after second retain: state=%q bytes=%q", state, resolved.Bytes)
	}
}

func TestSourceStoreExpiryTombstonesAndPurgeAreBounded(t *testing.T) {
	clock := newTestClock()
	store := newTestSourceStore(t, clock, 2, 64, time.Second, 1)
	first, err := store.Retain(testSourceRef(), []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	if _, state := store.Resolve(first.Ref.ID); state != SourceRefExpired {
		t.Fatalf("expired source state=%q", state)
	}
	if stats := store.Stats(); stats.Entries > 2 || stats.RetainedBytes > 64 || stats.Tombstones > 1 {
		t.Fatalf("unbounded store after expiry: %#v", stats)
	}

	second, err := store.Retain(testSourceRef(), []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Ref.ID == first.Ref.ID {
		t.Fatal("expired source id recycled")
	}
	clock.Advance(2 * time.Second)
	if _, state := store.Resolve(second.Ref.ID); state != SourceRefExpired {
		t.Fatalf("second expired source state=%q", state)
	}
	if _, state := store.Resolve(first.Ref.ID); state != SourceRefUnavailable {
		t.Fatalf("purged tombstone state=%q", state)
	}
	if stats := store.Stats(); stats.Tombstones != 1 {
		t.Fatalf("tombstone cap not enforced: %#v", stats)
	}
}

func TestSourceStoreEvictsToEntryAndByteBudgetsWithoutMutatingReturnedRefs(t *testing.T) {
	clock := newTestClock()
	store := newTestSourceStore(t, clock, 2, 5, time.Minute, 2)
	first, err := store.Retain(testSourceRef(), []byte("aa"))
	if err != nil {
		t.Fatal(err)
	}
	firstRef := first.Ref
	for _, data := range []string{"bb", "ccc"} {
		if _, err := store.Retain(testSourceRef(), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if first.Ref != firstRef {
		t.Fatalf("eviction mutated already-returned SourceRef: before=%#v after=%#v", firstRef, first.Ref)
	}
	if _, state := store.Resolve(first.Ref.ID); state != SourceRefUnavailable {
		t.Fatalf("capacity-evicted source state=%q", state)
	}
	stats := store.Stats()
	if stats.Entries > 2 || stats.RetainedBytes > 5 || stats.Tombstones > 2 {
		t.Fatalf("store budgets exceeded: %#v", stats)
	}
	if _, err := store.Retain(testSourceRef(), bytes.Repeat([]byte{'x'}, 6)); ErrorCode(err) != CodeQueryBudgetExceeded {
		t.Fatalf("oversized retain error=%v code=%q", err, ErrorCode(err))
	}
}

func newTestSourceStore(t *testing.T, clock *testClock, maxEntries int, maxBytes int64, ttl time.Duration, maxTombstones int) *SourceStore {
	t.Helper()
	ids := []core.SourceRefID{
		"src_01K00000000000000000000000",
		"src_01K00000000000000000000001",
		"src_01K00000000000000000000002",
		"src_01K00000000000000000000003",
		"src_01K00000000000000000000004",
	}
	next := 0
	store, err := NewSourceStore(SourceStoreConfig{
		MaxEntries:       maxEntries,
		MaxRetainedBytes: maxBytes,
		TTL:              ttl,
		MaxTombstones:    maxTombstones,
		Now:              clock.Now,
		NextID: func() core.SourceRefID {
			id := ids[next]
			next++
			return id
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testSourceRef() core.SourceRef {
	return core.SourceRef{
		Origin:            core.SourceWorkspace,
		RepositoryID:      workspace.RepositoryID("repo_01K00000000000000000000000"),
		WorkspaceID:       workspace.WorkspaceID("ws_01K00000000000000000000000"),
		LogicalPath:       "internal/app/service.go",
		DisplayIdentity:   "internal/app/service.go",
		ResolutionQuality: core.ResolutionExact,
		TextEncoding:      core.TextEncodingUTF8,
	}
}

type testClock struct{ now time.Time }

func newTestClock() *testClock               { return &testClock{now: time.Unix(1_700_000_000, 0)} }
func (c *testClock) Now() time.Time          { return c.now }
func (c *testClock) Advance(d time.Duration) { c.now = c.now.Add(d) }
