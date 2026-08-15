//go:build linux || darwin

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestA26MutationScopeRestartTTLAndNoSessionAuthorityReconstruction(t *testing.T) {
	ctx := context.Background()
	stateRoot := filepath.Join(t.TempDir(), "state")
	limits := storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024}
	store := openA26Store(t, stateRoot, limits)
	now := time.Now().UTC()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Workspace{
		SchemaVersion: workspace.SchemaVersion,
		ID:            "ws_01KZZ8AJJYRPX53ZX04P2NB9PM", RepositoryID: "repo_01KZZ8AJJYRPX53ZX04P2NB9PM",
		Label: "a26-restart", Root: repoRoot, GitDir: gitDir, CreatedAt: now, LastSeenAt: now,
	}
	if err := store.SaveWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}

	execution := daemonapp.NewService(store, processadapter.Owner{}, daemonapp.Options{
		Incarnation: "a26-before-restart", Shell: "/bin/sh", MaxQueuedInputBytes: 1024, TerminationGrace: 100 * time.Millisecond,
	})
	started, err := execution.Start(ctx, daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "a26-live-before-restart",
		Argv: []string{"/bin/sh", "-c", "sleep 30"}, CWD: t.TempDir(), YieldMS: 20, MaxOutputBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	live, err := execution.ResolveProcessSession(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !live.Known || !live.Current || live.PID <= 0 {
		t.Fatalf("pre-restart process authority=%#v", live)
	}

	scopes := daemonapp.NewMutationScopeService(store, nil)
	set, err := scopes.Set(ctx, mutationapp.SetRequest{
		MutationID: "mutation-restart", ScopeID: "scope-restart", ActivityID: "activity-restart",
		WorkspaceID: ws.ID, Mode: core.ModeMutate, Paths: []string{"src/**"}, TTLMS: 2 * core.MinTTL.Milliseconds(),
	})
	if err != nil || set.Scope == nil {
		t.Fatalf("set=%#v err=%v", set, err)
	}
	if err := execution.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	reopened := openA26Store(t, stateRoot, limits)
	reconstructedScopes := daemonapp.NewMutationScopeService(reopened, nil)
	afterRestart, err := reconstructedScopes.Inspect(ctx, mutationapp.InspectRequest{WorkspaceID: ws.ID})
	if err != nil || afterRestart.ActiveCount != 1 || len(afterRestart.ActiveScopes) != 1 || afterRestart.ActiveScopes[0].ScopeID != "scope-restart" {
		t.Fatalf("after restart=%#v err=%v", afterRestart, err)
	}

	restartedExecution := daemonapp.NewService(reopened, processadapter.Owner{}, daemonapp.Options{
		Incarnation: "a26-after-restart", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
	})
	resolution, err := restartedExecution.ResolveProcessSession(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.Known || resolution.Current || resolution.PID != 0 {
		t.Fatalf("restart reconstructed process authority: %#v", resolution)
	}

	wait := time.Until(set.Scope.ExpiresAt) + 150*time.Millisecond
	if wait > 0 {
		time.Sleep(wait)
	}
	expired, err := reconstructedScopes.Inspect(ctx, mutationapp.InspectRequest{WorkspaceID: ws.ID})
	if err != nil {
		t.Fatal(err)
	}
	if expired.ActiveCount != 0 || len(expired.ActiveScopes) != 0 {
		t.Fatalf("expired scope remained active: %#v", expired)
	}
}

func openA26Store(t *testing.T, root string, limits storeadapter.Limits) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
