//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	coreproject "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type projectWorkspaceLookup struct {
	values []workspace.Workspace
}

func (p projectWorkspaceLookup) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), p.values...), nil
}

type projectInspectActions struct {
	fakeActions
	project *projectapp.Service
}

func (a projectInspectActions) InspectProject(ctx context.Context, workspaceID string) (coreproject.Inspection, error) {
	return a.project.Inspect(ctx, workspaceID)
}

func TestManifestIPCV2InspectProjectRequiresWorkspaceID(t *testing.T) {
	valid := []byte(`{"ipc_version":2,"kind":"request","request_id":"project","action":"inspect.project","workspace_id":"ws_01K00000000000000000000000"}`)
	got, err := decodeRequestV2(bytesReaderV2(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "inspect.project" || got.WorkspaceID != "ws_01K00000000000000000000000" {
		t.Fatalf("decoded=%#v", got)
	}
	invalid := []byte(`{"ipc_version":2,"kind":"request","request_id":"project","action":"inspect.project"}`)
	if _, err := decodeRequestV2(bytesReaderV2(invalid)); err == nil {
		t.Fatal("inspect.project without workspace_id accepted")
	}
}

func TestInspectionDoesNotExecuteManifestCommandThroughIPC(t *testing.T) {
	root := t.TempDir()
	sentinel := filepath.Join(root, "SENTINEL")
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "schema_version=1\n[commands.never_run]\nshell=\"touch " + sentinel + "\"\n"
	if err := os.WriteFile(filepath.Join(root, ".shellbeam", "project.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	const workspaceID = "ws_01K00000000000000000000000"
	projectSvc := projectapp.New(projectWorkspaceLookup{values: []workspace.Workspace{{ID: workspaceID, Root: root}}}, projectadapter.NewLoader())
	actions := projectInspectActions{project: projectSvc}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-project-ipc-")
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
	got, err := NewClient(server.SocketPath()).CallV2(context.Background(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "project", Action: "inspect.project", WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Project == nil || got.Project.Status != coreproject.StatusReviewDue {
		t.Fatalf("response=%#v", got)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("manifest command executed through IPC inspect: %v", err)
	}
}
