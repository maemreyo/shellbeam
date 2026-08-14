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
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceProvenanceBindsPrePostGeneration(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	pre := daemonSnapshot(t, strings.Repeat("a", 40), workspace.QualityFresh)
	post := daemonSnapshot(t, strings.Repeat("b", 40), workspace.QualityFresh)
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{pre, post}}
	owner := &fakeOwner{}
	svc := app.NewServiceWithWorkspaceObserver(store, owner, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-workspace-provenance", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.WorkspaceProvenance == nil {
		t.Fatalf("receipt=%#v", terminal.Receipt)
	}
	got := terminal.Receipt.WorkspaceProvenance
	if got.PreGeneration != pre.Generation || got.PostGeneration != post.Generation || !got.ObservedChange {
		t.Fatalf("provenance=%#v", got)
	}
	if observer.CallCount() != 2 || owner.starts.Load() != 1 {
		t.Fatalf("observer=%d starts=%d", observer.CallCount(), owner.starts.Load())
	}
}

func TestWorkspaceObservationUnavailableNeverChangesChildOutcome(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	unavailable := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, Quality: workspace.QualityUnavailable, ObservedAt: now, DiagnosticCode: "observation_budget_exceeded"}
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{unavailable, unavailable}}
	owner := &fakeOwner{}
	svc := app.NewServiceWithWorkspaceObserver(store, owner, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-workspace-unavailable", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.Outcome != "success" || terminal.Receipt == nil || terminal.Receipt.WorkspaceProvenance == nil {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Receipt.WorkspaceProvenance.PreQuality != workspace.QualityUnavailable || terminal.Receipt.WorkspaceProvenance.ObservedChange {
		t.Fatalf("provenance=%#v", terminal.Receipt.WorkspaceProvenance)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestWorkspaceObservationRetryDoesNotRebindExistingOperation(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	pre := daemonSnapshot(t, strings.Repeat("a", 40), workspace.QualityFresh)
	post := daemonSnapshot(t, strings.Repeat("b", 40), workspace.QualityFresh)
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{pre, post, daemonSnapshot(t, strings.Repeat("c", 40), workspace.QualityFresh)}}
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
	if observer.CallCount() != calls || calls != 2 || owner.starts.Load() != 1 {
		t.Fatalf("observer=%d callsBefore=%d starts=%d", observer.CallCount(), calls, owner.starts.Load())
	}
}

type sequenceWorkspaceObserver struct {
	mu        sync.Mutex
	snapshots []workspace.FastSnapshot
	calls     int
}

func (o *sequenceWorkspaceObserver) Observe(context.Context, string) workspace.FastSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
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
