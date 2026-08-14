package git

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
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

func TestAgentExecutionA1FreshSnapshotBypassesWarmTTLAndRefreshesCache(t *testing.T) {
	clock := newSnapshotClock()
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: 50 * time.Millisecond, Now: clock.Now})
	workspace := cacheWorkspace()
	first := adapter.Snapshot(context.Background(), workspace)
	runner.SetOutput([]byte("# branch.oid " + strings.Repeat("b", 40) + "\x00# branch.head feature\x00"))
	cached := adapter.Snapshot(context.Background(), workspace)
	if cached.Generation != first.Generation || cached.Quality != core.QualityCached {
		t.Fatalf("warm snapshot unexpectedly refreshed: first=%#v cached=%#v", first, cached)
	}
	fresh := adapter.SnapshotFresh(context.Background(), workspace)
	if fresh.Quality != core.QualityFresh || fresh.Ref != "refs/heads/feature" || fresh.Generation == first.Generation {
		t.Fatalf("fresh snapshot=%#v first=%#v", fresh, first)
	}
	after := adapter.Snapshot(context.Background(), workspace)
	if after.Quality != core.QualityCached || after.Generation != fresh.Generation {
		t.Fatalf("refreshed cache=%#v fresh=%#v", after, fresh)
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

func TestSnapshotCachedNeverSpawnsGitForEmptyOrStaleCache(t *testing.T) {
	clock := newSnapshotClock()
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: 100 * time.Millisecond, Budget: 50 * time.Millisecond, Now: clock.Now})
	workspace := cacheWorkspace()

	empty := adapter.SnapshotCached(context.Background(), workspace)
	if empty.Quality != core.QualityUnavailable || empty.DiagnosticCode != "workspace_cache_empty" || runner.CallCount() != 0 {
		t.Fatalf("empty=%#v calls=%d", empty, runner.CallCount())
	}

	fresh := adapter.SnapshotFresh(context.Background(), workspace)
	calls := runner.CallCount()
	if fresh.Quality != core.QualityFresh || calls == 0 {
		t.Fatalf("fresh=%#v calls=%d", fresh, calls)
	}

	cached := adapter.SnapshotCached(context.Background(), workspace)
	if cached.Quality != core.QualityCached || cached.Generation != fresh.Generation || runner.CallCount() != calls {
		t.Fatalf("cached=%#v calls=%d wantCalls=%d", cached, runner.CallCount(), calls)
	}

	clock.Advance(time.Second)
	stale := adapter.SnapshotCached(context.Background(), workspace)
	if stale.Quality != core.QualityStale || stale.Generation != fresh.Generation || stale.CacheAgeMS < 1000 || runner.CallCount() != calls {
		t.Fatalf("stale=%#v calls=%d wantCalls=%d", stale, runner.CallCount(), calls)
	}
}

func TestOrdinaryDaemonStartPaysZeroGitWorkspaceFreshnessTax(t *testing.T) {
	runner := &snapshotRunner{output: cleanStatusOutput()}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: time.Second, Now: time.Now})
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{
		MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	root := t.TempDir()
	workspace := core.Workspace{
		SchemaVersion: core.SchemaVersion,
		ID:            core.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID:  core.RepositoryID("repo_01K00000000000000000000000"),
		Label:         "no-tax",
		Root:          root,
		GitDir:        filepath.Join(root, ".git"),
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	observer := workspaceapp.NewObserver(store, adapter)
	svc := daemonapp.NewServiceWithWorkspaceObserver(store, noTaxOwner{}, observer, daemonapp.Options{
		Incarnation: "no-tax-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 128,
	})
	started, err := svc.Start(context.Background(), daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "no-tax-start", Command: "true", CWD: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitNoTaxTerminal(t, svc, started.SessionID)
	if runner.CallCount() != 0 {
		t.Fatalf("ordinary start spawned workspace Git commands: %#v", runner.Args())
	}
	got := terminal.Receipt.WorkspaceProvenance
	if got == nil || got.Pre.Kind != receipt.WorkspaceUnreconciled || got.Post.Kind != receipt.WorkspaceUnreconciled || !got.Post.ObservationInvalidated {
		t.Fatalf("workspace provenance=%#v", got)
	}
}

func waitNoTaxTerminal(t *testing.T, svc *daemonapp.Service, sessionID string) daemonapp.View {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		view, err := svc.Poll(context.Background(), daemonapp.PollRequest{SessionID: sessionID, YieldMS: 10, MaxOutputBytes: 1024})
		if err != nil {
			t.Fatal(err)
		}
		if view.State.Terminal() {
			return view
		}
	}
	t.Fatal("no-tax command did not become terminal")
	return daemonapp.View{}
}

type noTaxOwner struct{}

func (noTaxOwner) Start(context.Context, operation.ExecutionSpec, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	return noTaxHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type noTaxHandle struct{}

func (noTaxHandle) Write([]byte) error                   { return nil }
func (noTaxHandle) CloseStdin() error                    { return nil }
func (noTaxHandle) Signal(string) receipt.SignalEvidence { return receipt.SignalEvidence{} }
func (noTaxHandle) Wait(context.Context) receipt.ExitEvidence {
	code := 0
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (noTaxHandle) Close() error { return nil }
