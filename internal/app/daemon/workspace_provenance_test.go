package daemon_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceLazyProvenanceUsesCachedPreAndUnreconciledPost(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	pre := daemonSnapshot(t, strings.Repeat("a", 40), workspace.QualityCached)
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{pre}}
	owner := &fakeOwner{}
	svc := app.NewServiceWithWorkspaceObserver(store, owner, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-workspace-provenance", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	got := terminal.Receipt.WorkspaceProvenance
	if got == nil || got.SchemaVersion != 2 || got.Pre.Kind != receipt.WorkspaceCached || got.Pre.Generation != pre.Generation || got.Post.Kind != receipt.WorkspaceUnreconciled || !got.Post.ObservationInvalidated || got.ObservedChange {
		t.Fatalf("provenance=%#v", got)
	}
	if observer.CallCount() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("observer=%d starts=%d", observer.CallCount(), owner.starts.Load())
	}
}

func TestWorkspaceLazyObservationUnavailableNeverChangesChildOutcome(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, Quality: workspace.QualityUnavailable, ObservedAt: time.Now().UTC(), DiagnosticCode: "workspace_cache_empty"}
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{unavailable}}
	svc := app.NewServiceWithWorkspaceObserver(store, &fakeOwner{}, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-workspace-unavailable", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	got := terminal.Receipt.WorkspaceProvenance
	if terminal.Outcome != "success" || got == nil || got.Pre.Kind != receipt.WorkspaceUnreconciled || got.Pre.DiagnosticCode != "workspace_cache_empty" || got.ObservedChange {
		t.Fatalf("terminal=%#v provenance=%#v", terminal, got)
	}
}

func TestWorkspaceLazyRetryDoesNotRebindWithoutHint(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	pre := daemonSnapshot(t, strings.Repeat("a", 40), workspace.QualityCached)
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{pre, daemonSnapshot(t, strings.Repeat("b", 40), workspace.QualityCached)}}
	owner := &fakeOwner{}
	svc := app.NewServiceWithWorkspaceObserver(store, owner, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "op-workspace-retry", Command: "true", CWD: "/repo"}
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, started.SessionID)
	calls := observer.CallCount()
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if observer.CallCount() != calls || calls != 1 || owner.starts.Load() != 1 {
		t.Fatalf("observer=%d callsBefore=%d starts=%d", observer.CallCount(), calls, owner.starts.Load())
	}
}

type sequenceWorkspaceObserver struct {
	mu        sync.Mutex
	snapshots []workspace.FastSnapshot
	calls     int
	binds     int
}

func (o *sequenceWorkspaceObserver) Bind(context.Context, string) workspace.Binding {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.binds++
	if len(o.snapshots) == 0 {
		return workspace.Binding{}
	}
	snapshot := o.snapshots[0]
	return workspace.Binding{RepositoryID: snapshot.RepositoryID, WorkspaceID: snapshot.WorkspaceID}
}
func (o *sequenceWorkspaceObserver) ObserveCached(context.Context, string) workspace.FastSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.snapshots) == 0 {
		return workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, Quality: workspace.QualityUnavailable, ObservedAt: time.Now().UTC(), DiagnosticCode: "workspace_cache_empty"}
	}
	index := o.calls
	o.calls++
	if index >= len(o.snapshots) {
		return o.snapshots[len(o.snapshots)-1]
	}
	return o.snapshots[index]
}
func (o *sequenceWorkspaceObserver) CallCount() int { o.mu.Lock(); defer o.mu.Unlock(); return o.calls }

func daemonSnapshot(t *testing.T, head string, quality workspace.ObservationQuality) workspace.FastSnapshot {
	t.Helper()
	snapshot := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"), Head: head, Ref: "refs/heads/main", Dirty: workspace.DirtySummary{Digest: strings.Repeat("d", 64)}, Quality: quality, ObservedAt: time.Now().UTC()}
	got, err := workspace.WithGeneration(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
