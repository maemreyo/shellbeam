package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestAgentExecutionA1ActivityCrossesRegisteredWorkspaces(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	first, err := workspaceService.Attach(context.Background(), initWorkspaceCLIRepo(t), "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspaceService.Attach(context.Background(), initWorkspaceCLIRepo(t), "second")
	if err != nil {
		t.Fatal(err)
	}
	client := runA1Daemon(t, stateDir, runtimeDir)

	firstResult := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a1-cross-first", ActivityID: "activity-cross",
		WorkspaceID: string(first.ID), CWD: ".", Command: "pwd",
	})
	secondResult := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a1-cross-second", ActivityID: "activity-cross",
		WorkspaceID: string(second.ID), CWD: ".", Command: "pwd",
	})
	assertA1ChildSuccess(t, firstResult)
	assertA1ChildSuccess(t, secondResult)

	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "activity-cross",
		Action: "inspect.activity", ActivityID: "activity-cross",
	})
	if err != nil || !response.OK || response.Activity == nil {
		t.Fatalf("inspect activity response=%#v err=%v", response, err)
	}
	if len(response.Activity.Operations) != 2 || len(response.Activity.WorkspaceIDs) != 2 {
		t.Fatalf("cross-workspace activity=%#v", response.Activity)
	}
	seen := map[string]bool{}
	for _, id := range response.Activity.WorkspaceIDs {
		seen[string(id)] = true
	}
	if !seen[string(first.ID)] || !seen[string(second.ID)] {
		t.Fatalf("activity workspace ids=%v want %s and %s", response.Activity.WorkspaceIDs, first.ID, second.ID)
	}
}

func TestAgentExecutionA1GitMutationsRemainExecutableWithHonestProvenance(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	record, err := workspaceService.Attach(context.Background(), initWorkspaceCLIRepo(t), "git-mutations")
	if err != nil {
		t.Fatal(err)
	}
	client := runA1Daemon(t, stateDir, runtimeDir)

	rootProbe := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a1-git-root-probe", ActivityID: "activity-git-mutations",
		WorkspaceID: string(record.ID), CWD: ".", Command: "true",
	})
	assertA1ChildSuccess(t, rootProbe)
	if rootProbe.Receipt == nil || rootProbe.Receipt.CWD != record.Root {
		t.Fatalf("workspace root probe cwd=%q want %q", rootProbe.Receipt.CWD, record.Root)
	}

	switchResult := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a1-git-switch", ActivityID: "activity-git-mutations",
		WorkspaceID: string(record.ID), CWD: ".", Argv: []string{"git", "switch", "-c", "a1-runtime-branch"},
	})
	assertA1ChildSuccess(t, switchResult)
	if switchResult.Receipt == nil || switchResult.Receipt.WorkspaceProvenance == nil {
		t.Fatalf("switch provenance=%#v", switchResult.Receipt)
	}
	provenance := switchResult.Receipt.WorkspaceProvenance
	if provenance.SchemaVersion != 2 || provenance.Binding.WorkspaceID != record.ID ||
		provenance.Pre.Kind != receipt.WorkspaceUnreconciled || provenance.Post.Kind != receipt.WorkspaceUnreconciled ||
		!provenance.Post.ObservationInvalidated || provenance.ObservedChange || provenance.PreGeneration != "" || provenance.PostGeneration != "" {
		t.Fatalf("switch provenance=%#v", provenance)
	}

	commands := []ipcadapter.RequestV2{
		{Action: "start", OperationID: "a1-git-dirty", ActivityID: "activity-git-mutations", WorkspaceID: string(record.ID), CWD: ".", Command: "printf 'dirty\\n' >> README"},
		{Action: "start", OperationID: "a1-git-stash", ActivityID: "activity-git-mutations", WorkspaceID: string(record.ID), CWD: ".", Argv: []string{"git", "stash", "push", "-m", "a1-runtime"}},
		{Action: "start", OperationID: "a1-git-reset", ActivityID: "activity-git-mutations", WorkspaceID: string(record.ID), CWD: ".", Argv: []string{"git", "reset", "--hard", "HEAD"}},
		{Action: "start", OperationID: "a1-git-rebase", ActivityID: "activity-git-mutations", WorkspaceID: string(record.ID), CWD: ".", Argv: []string{"git", "rebase", "HEAD"}},
	}
	for _, request := range commands {
		result := callA1Terminal(t, client, request)
		assertA1ChildSuccess(t, result)
		if result.Receipt == nil || result.Receipt.WorkspaceProvenance == nil {
			t.Fatalf("%s missing workspace provenance: %#v", request.OperationID, result.Receipt)
		}
	}
}

func a1RuntimeDirs(t *testing.T) (string, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shellbeam-a1-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return filepath.Join(root, "state"), filepath.Join(root, "run")
}

func openA1Store(t *testing.T, stateDir string) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(stateDir, storeadapter.Limits{
		MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func runA1Daemon(t *testing.T, stateDir, runtimeDir string) *ipcadapter.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}
	go func() { done <- runDaemon(ctx, args) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	return ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock"))
}

func callA1Terminal(t *testing.T, client *ipcadapter.Client, request ipcadapter.RequestV2) receipt.Result {
	t.Helper()
	request.IPVersion = 2
	request.Kind = "request"
	request.RequestID = request.OperationID
	request.YieldMS = 1000
	request.MaxOutputBytes = 4096
	response, err := client.CallV2(context.Background(), request)
	if err != nil || !response.OK || response.Result == nil {
		t.Fatalf("%s start response=%#v err=%v", request.OperationID, response, err)
	}
	result := *response.Result
	cursor := result.Output.NextCursor
	for attempt := 0; attempt < 12 && result.Operation.State != receipt.OperationTerminal; attempt++ {
		poll, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: request.OperationID + "-poll",
			Action: "poll", SessionID: result.Operation.SessionID, Cursor: cursor,
			YieldMS: 1000, MaxOutputBytes: 4096,
		})
		if err != nil || !poll.OK || poll.Result == nil {
			t.Fatalf("%s poll response=%#v envelope=%#v err=%v", request.OperationID, poll, poll.Error, err)
		}
		result = *poll.Result
		cursor = result.Output.NextCursor
	}
	if result.Operation.State != receipt.OperationTerminal {
		t.Fatalf("%s did not become terminal: %#v", request.OperationID, result)
	}
	return result
}

func assertA1ChildSuccess(t *testing.T, result receipt.Result) {
	t.Helper()
	if result.Child == nil || result.Child.State != receipt.ChildExited || result.Child.Outcome != session.Success ||
		result.Child.ExitCode == nil || *result.Child.ExitCode != 0 {
		t.Fatalf("child result=%#v receipt=%#v", result.Child, result.Receipt)
	}
}
