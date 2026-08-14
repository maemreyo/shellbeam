//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type a1InspectActions struct {
	fakeActions
	workspace  workspace.Workspace
	activity   activity.Activity
	startCalls int
}

func (a *a1InspectActions) Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error) {
	a.startCalls++
	return daemonapp.View{SessionID: "unexpected"}, nil
}

func (a *a1InspectActions) InspectWorkspace(_ context.Context, id string) (workspace.Workspace, error) {
	if id != string(a.workspace.ID) {
		return workspace.Workspace{}, errors.New("workspace not found")
	}
	return a.workspace, nil
}

func (a *a1InspectActions) InspectActivity(_ context.Context, id string) (activity.Activity, error) {
	if id != string(a.activity.ID) {
		return activity.Activity{}, errors.New("activity not found")
	}
	return a.activity, nil
}

func TestAgentExecutionA1IPCV2InspectWorkspaceAndActivityNeverSpawn(t *testing.T) {
	now := time.Date(2026, 8, 14, 3, 30, 0, 0, time.UTC)
	actions := &a1InspectActions{
		workspace: workspace.Workspace{
			SchemaVersion: workspace.SchemaVersion,
			ID:            "ws_01K00000000000000000000000",
			RepositoryID:  "repo_01K00000000000000000000000",
			Label:         "primary",
			Root:          "/tmp/repo",
			GitDir:        "/tmp/repo/.git",
			CreatedAt:     now,
			LastSeenAt:    now,
		},
		activity: activity.Activity{
			SchemaVersion: activity.SchemaVersion,
			ID:            "activity-a1",
			WorkspaceIDs:  []workspace.WorkspaceID{"ws_01K00000000000000000000000"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-a1-inspect-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	server, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()

	client := NewClient(server.SocketPath())
	gotWorkspace, err := client.CallV2(context.Background(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "workspace",
		Action: "inspect.workspace", WorkspaceID: string(actions.workspace.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gotWorkspace.OK || gotWorkspace.Workspace == nil || gotWorkspace.Workspace.ID != actions.workspace.ID {
		t.Fatalf("workspace response=%#v", gotWorkspace)
	}

	gotActivity, err := client.CallV2(context.Background(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "activity",
		Action: "inspect.activity", ActivityID: string(actions.activity.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gotActivity.OK || gotActivity.Activity == nil || gotActivity.Activity.ID != actions.activity.ID {
		t.Fatalf("activity response=%#v", gotActivity)
	}
	if actions.startCalls != 0 {
		t.Fatalf("inspect spawned command: starts=%d", actions.startCalls)
	}
}

func TestAgentExecutionA1InspectRequestsRequireTypedIDs(t *testing.T) {
	cases := []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "workspace", Action: "inspect.workspace"},
		{IPVersion: 2, Kind: "request", RequestID: "activity", Action: "inspect.activity"},
	}
	for _, request := range cases {
		if err := validateRequestV2(request); err == nil {
			t.Fatalf("%s without id accepted", request.Action)
		}
	}
}
