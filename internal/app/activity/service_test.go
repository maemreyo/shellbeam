package activity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestActivityLazyCreationReuseAndWorkspaceBaselines(t *testing.T) {
	registry := newMemoryRegistry()
	source := &baselineSource{}
	service := New(registry, source, 4)
	now := time.Now().UTC()
	ws1 := workspace.WorkspaceID("ws_01K00000000000000000000000")
	ws2 := workspace.WorkspaceID("ws_01K00000000000000000000001")
	for i, ws := range []workspace.WorkspaceID{ws1, ws1, ws2} {
		got, err := service.Admit(context.Background(), core.Admission{ActivityID: core.ID("ZMR-111-validator"), OperationID: string(rune(97 + i)), SessionID: string(rune(107 + i)), WorkspaceID: ws, CWD: "/repo", ObservedAt: now.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != core.ID("ZMR-111-validator") {
			t.Fatalf("activity=%#v", got)
		}
	}
	got, found, err := registry.LoadActivity(context.Background(), core.ID("ZMR-111-validator"))
	if err != nil || !found {
		t.Fatalf("load found=%v err=%v", found, err)
	}
	if len(got.Operations) != 3 || len(got.Baselines) != 2 || len(got.WorkspaceIDs) != 2 {
		t.Fatalf("activity=%#v", got)
	}
	if source.Calls(ws1) != 1 || source.Calls(ws2) != 1 {
		t.Fatalf("baseline calls ws1=%d ws2=%d", source.Calls(ws1), source.Calls(ws2))
	}
}

func TestActivityConcurrentIndependentIDsDoNotOwnWorkspace(t *testing.T) {
	registry := newMemoryRegistry()
	source := &baselineSource{}
	service := New(registry, source, 8)
	ws := workspace.WorkspaceID("ws_01K00000000000000000000000")
	var wg sync.WaitGroup
	for _, id := range []core.ID{"task-a", "task-b"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Admit(context.Background(), core.Admission{ActivityID: id, OperationID: "op-" + string(id), SessionID: "s-" + string(id), WorkspaceID: ws, CWD: "/repo", ObservedAt: time.Now().UTC()}); err != nil {
				t.Errorf("Admit(%s): %v", id, err)
			}
		}()
	}
	wg.Wait()
	if registry.Count() != 2 {
		t.Fatalf("activity count=%d", registry.Count())
	}
}

type memoryRegistry struct {
	mu      sync.Mutex
	records map[core.ID]core.Activity
}

func newMemoryRegistry() *memoryRegistry {
	return &memoryRegistry{records: map[core.ID]core.Activity{}}
}
func (r *memoryRegistry) LoadActivity(_ context.Context, id core.ID) (core.Activity, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.records[id]
	return got, ok, nil
}
func (r *memoryRegistry) SaveActivity(_ context.Context, record core.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[record.ID] = record
	return nil
}
func (r *memoryRegistry) Count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.records) }

type baselineSource struct {
	mu    sync.Mutex
	calls map[workspace.WorkspaceID]int
}

func (s *baselineSource) CaptureBaseline(_ context.Context, ws workspace.WorkspaceID, _ string) core.Observation {
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[workspace.WorkspaceID]int{}
	}
	s.calls[ws]++
	s.mu.Unlock()
	return core.Observation{WorkspaceID: ws, Ref: "refs/heads/main", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Quality: workspace.QualityFresh, ObservedAt: time.Now().UTC(), Paths: []core.PathFact{{Path: "dirty.go", State: core.PathModified}}}
}
func (s *baselineSource) Calls(ws workspace.WorkspaceID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[ws]
}

func TestActivityInspectReturnsExistingRecordAndTypedNotFound(t *testing.T) {
	registry := newMemoryRegistry()
	service := New(registry, nil, 4)
	now := time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)
	record := core.New(core.ID("activity-a1"), now)
	if err := registry.SaveActivity(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	got, err := service.Inspect(context.Background(), "activity-a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != record.ID || !got.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("inspect=%#v", got)
	}
	if _, err := service.Inspect(context.Background(), "missing"); !errors.Is(err, failure.ActivityNotFound) {
		t.Fatalf("missing err=%v", err)
	}
	if _, err := service.Inspect(context.Background(), "bad/id"); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("invalid id err=%v", err)
	}
}
