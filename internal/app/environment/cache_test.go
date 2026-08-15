package environment

import (
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
)

func TestSnapshotCacheClonesValuesAndEvictsBindingIndex(t *testing.T) {
	cache := newSnapshotCache(1)
	first := core.Snapshot{CapturedAt: time.Unix(1, 0), VariablePresence: []core.VariablePresence{{Name: "CI", Present: true}}}
	cache.put("one", "binding-one", first)
	got, ok := cache.get("one")
	if !ok {
		t.Fatal("missing cached snapshot")
	}
	got.VariablePresence[0].Name = "MUTATED"
	again, _ := cache.get("one")
	if again.VariablePresence[0].Name != "CI" {
		t.Fatal("cache returned aliased snapshot")
	}
	cache.put("two", "binding-two", core.Snapshot{CapturedAt: time.Unix(2, 0)})
	if _, ok := cache.getByBinding("binding-one"); ok {
		t.Fatal("evicted snapshot retained binding index")
	}
	if cache.size() != 1 {
		t.Fatalf("size=%d", cache.size())
	}
}
