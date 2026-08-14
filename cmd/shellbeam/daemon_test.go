package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"github.com/maemreyo/shellbeam/internal/app/control"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestSecondDaemonDoesNotReconcileLiveOwnersState(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shellbeam-daemon-race-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- runDaemon(ctx, args) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))

	repo, err := storeadapter.Open(stateDir, storeadapter.Limits{
		MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	reservation := operation.Reservation{
		SchemaVersion: 1, OperationID: "live-operation", SessionID: "live-session",
		Fingerprint: "live-fingerprint", Command: "sleep 60", CWD: root,
		Shell: "/bin/sh", DaemonIncarnation: "live-daemon",
	}
	if _, _, result := repo.ReserveOperation(context.Background(), reservation); result.Err != nil {
		t.Fatal(result.Err)
	}
	before := sessionTree(t, filepath.Join(stateDir, "sessions", "live-session"))

	err = runDaemon(context.Background(), args)
	if err == nil || err.Error() != "daemon_already_running" {
		t.Fatalf("second daemon error = %v", err)
	}
	after := sessionTree(t, filepath.Join(stateDir, "sessions", "live-session"))
	if !bytes.Equal(before, after) {
		t.Fatalf("second daemon modified live state\nbefore: %q\nafter:  %q", before, after)
	}

	cancel()
	select {
	case err = <-firstDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("first daemon shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first daemon did not stop")
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path did not appear: %s", path)
}

func sessionTree(t *testing.T, root string) []byte {
	t.Helper()
	var paths []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var out []byte
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rel...)
		out = append(out, 0)
		out = append(out, b...)
		out = append(out, 0)
	}
	return out
}

func TestAgentExecutionA1DaemonComposesWorkspaceActivityInspectAndExecution(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	workspaceRecord, err := workspaceService.Attach(context.Background(), initWorkspaceCLIRepo(t), "primary")
	if err != nil {
		t.Fatal(err)
	}
	seed := activity.New(activity.ID("activity-seed"), time.Now().UTC())
	if err := store.SaveActivity(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	client := runA1Daemon(t, stateDir, runtimeDir)

	serverInfo, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "server", Action: "inspect.server",
	})
	if err != nil || !serverInfo.OK || serverInfo.Server == nil {
		t.Fatalf("inspect.server response=%#v err=%v", serverInfo, err)
	}
	for _, feature := range []capability.Feature{
		capability.FeatureWorkspaceAddressing, capability.FeatureWorkspaceProvenance, capability.FeatureActivities,
	} {
		if serverInfo.Server.Features[feature] != capability.Available {
			t.Fatalf("feature %s=%s", feature, serverInfo.Server.Features[feature])
		}
	}
	workspaceResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "workspace", Action: "inspect.workspace", WorkspaceID: string(workspaceRecord.ID),
	})
	if err != nil || !workspaceResponse.OK || workspaceResponse.Workspace == nil || workspaceResponse.Workspace.ID != workspaceRecord.ID {
		t.Fatalf("inspect.workspace response=%#v err=%v", workspaceResponse, err)
	}
	activityResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "activity", Action: "inspect.activity", ActivityID: "activity-seed",
	})
	if err != nil || !activityResponse.OK || activityResponse.Activity == nil || activityResponse.Activity.ID != seed.ID {
		t.Fatalf("inspect.activity response=%#v err=%v", activityResponse, err)
	}

	start, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "start", Action: "start", OperationID: "a1-runtime-start",
		ActivityID: "activity-runtime", WorkspaceID: string(workspaceRecord.ID), CWD: ".", Command: "pwd", YieldMS: 2000, MaxOutputBytes: 4096,
	})
	if err != nil || !start.OK || start.Result == nil {
		t.Fatalf("workspace start response=%#v err=%v", start, err)
	}
	if start.Result.Operation.WorkspaceID != string(workspaceRecord.ID) || start.Result.Operation.ActivityID != "activity-runtime" {
		t.Fatalf("operation=%#v", start.Result.Operation)
	}
	tracked, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "tracked", Action: "inspect.activity", ActivityID: "activity-runtime",
	})
	if err != nil || !tracked.OK || tracked.Activity == nil || len(tracked.Activity.Operations) != 1 {
		t.Fatalf("tracked activity response=%#v err=%v", tracked, err)
	}
	if tracked.Activity.Operations[0].WorkspaceID != workspaceRecord.ID {
		t.Fatalf("tracked activity=%#v", tracked.Activity)
	}
}

func TestAgentExecutionA1DoctorRequiresDaemonIPCRoundTrip(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shellbeam-doctor-a1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
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

	report, err := doctorReport(args)
	if err != nil {
		t.Fatal(err)
	}
	check := doctorCheckByID(t, report, "socket")
	if check.Status != control.Pass || check.Message != "daemon IPC responsive" {
		t.Fatalf("live socket check=%#v", check)
	}
}

func TestAgentExecutionA1DoctorDoesNotTrustStaleSocketInode(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shellbeam-doctor-stale-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(runtimeDir, "daemon.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket missing: info=%v err=%v", info, err)
	}

	report, err := doctorReport([]string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	check := doctorCheckByID(t, report, "socket")
	if check.Status == control.Pass {
		t.Fatalf("stale socket trusted as healthy: %#v", check)
	}
}

func doctorCheckByID(t *testing.T, report control.Report, id string) control.Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing doctor check %q in %#v", id, report)
	return control.Check{}
}
