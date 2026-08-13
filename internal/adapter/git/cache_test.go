package git

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestCacheWarmHitUsesZeroSubprocesses(t *testing.T) {
	clock := newSnapshotClock()
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: 50 * time.Millisecond, Now: clock.Now})
	workspace := cacheWorkspace()
	first := adapter.Snapshot(context.Background(), workspace)
	coldCalls := runner.CallCount()
	second := adapter.Snapshot(context.Background(), workspace)
	if coldCalls < 1 || coldCalls > 2 {
		t.Fatalf("cold subprocesses=%d", coldCalls)
	}
	if runner.CallCount() != coldCalls {
		t.Fatalf("warm cache spawned subprocess")
	}
	if first.Quality != core.QualityFresh || second.Quality != core.QualityCached || second.Generation != first.Generation {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestObservationBudgetStaleMalformedFallsBackToCachedSnapshot(t *testing.T) {
	clock := newSnapshotClock()
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: 100 * time.Millisecond, Budget: 20 * time.Millisecond, Now: clock.Now})
	first := adapter.Snapshot(context.Background(), cacheWorkspace())
	clock.Advance(time.Second)
	runner.SetOutput([]byte("malformed\x00"))
	second := adapter.Snapshot(context.Background(), cacheWorkspace())
	if second.Quality != core.QualityStale || second.Generation != first.Generation || second.DiagnosticCode != "git_status_malformed" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if runner.CallCount() > 2 {
		t.Fatalf("subprocess budget exceeded: %d", runner.CallCount())
	}
}

func TestObservationBudgetTimeoutWithoutCacheIsUnavailable(t *testing.T) {
	clock := newSnapshotClock()
	runner := &snapshotRunner{block: true}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: 10 * time.Millisecond, Now: clock.Now})
	got := adapter.Snapshot(context.Background(), cacheWorkspace())
	if got.Quality != core.QualityUnavailable || got.DiagnosticCode != "observation_budget_exceeded" {
		t.Fatalf("snapshot=%#v", got)
	}
	if runner.CallCount() > 2 {
		t.Fatalf("subprocess budget exceeded: %d", runner.CallCount())
	}
}

func TestObservationNeverRunsFetchOrExternalIdentityProbes(t *testing.T) {
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: time.Second, Now: time.Now})
	_ = adapter.Snapshot(context.Background(), cacheWorkspace())
	for _, args := range runner.Args() {
		joined := strings.Join(args, " ")
		for _, forbidden := range []string{"fetch", "ssh", "gh", "credential"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("forbidden observation command: %q", joined)
			}
		}
	}
}

type snapshotRunner struct {
	mu     sync.Mutex
	calls  int
	output []byte
	block  bool
	args   [][]string
}

func (r *snapshotRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	r.calls++
	r.args = append(r.args, append([]string(nil), args...))
	output := append([]byte(nil), r.output...)
	block := r.block
	r.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return output, nil, nil
}
func (r *snapshotRunner) CallCount() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
func (r *snapshotRunner) SetOutput(v []byte) {
	r.mu.Lock()
	r.output = append([]byte(nil), v...)
	r.mu.Unlock()
}
func (r *snapshotRunner) Args() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.args))
	for i := range r.args {
		out[i] = append([]string(nil), r.args[i]...)
	}
	return out
}

type snapshotClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSnapshotClock() *snapshotClock {
	return &snapshotClock{now: time.Date(2026, 8, 13, 17, 0, 0, 0, time.UTC)}
}
func (c *snapshotClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *snapshotClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func cacheWorkspace() core.Workspace {
	now := time.Date(2026, 8, 13, 17, 0, 0, 0, time.UTC)
	return core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: core.RepositoryID("repo_01K00000000000000000000000"), Label: "cache", Root: "/repo", GitDir: "/repo/.git", CreatedAt: now, LastSeenAt: now}
}
func cleanStatusOutput() []byte {
	return []byte("# branch.oid " + strings.Repeat("a", 40) + "\x00# branch.head main\x00")
}
