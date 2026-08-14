package project

import (
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeWorkspaceLookup struct {
	values []workspace.Workspace
}

func (f fakeWorkspaceLookup) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), f.values...), nil
}

type fakeLoader struct {
	result core.LoadResult
	root   string
	calls  int
}

func (f *fakeLoader) Load(_ context.Context, root string) core.LoadResult {
	f.calls++
	f.root = root
	return f.result
}

func TestProjectStatusInspectUsesRegisteredWorkspaceRoot(t *testing.T) {
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &fakeLoader{result: core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: "raw"}}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{{
		ID: "ws_01K00000000000000000000000", Root: "/repo",
	}}}, loader)
	got, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 || loader.root != "/repo" {
		t.Fatalf("loader calls=%d root=%q", loader.calls, loader.root)
	}
	if got.Status != core.StatusValid || got.ManifestDigest != "raw" || got.DiscoveryFingerprint != parsed.Fingerprint || got.Manifest == nil {
		t.Fatalf("inspection=%#v", got)
	}
}

func TestProjectStatusInspectReturnsAbsentOrInvalidAsData(t *testing.T) {
	workspaceLookup := fakeWorkspaceLookup{values: []workspace.Workspace{{ID: "ws_01K00000000000000000000000", Root: "/repo"}}}
	for _, result := range []core.LoadResult{
		{State: core.LoadAbsent},
		{State: core.LoadInvalid, Code: core.CodeParseError, ManifestDigest: "raw"},
	} {
		loader := &fakeLoader{result: result}
		got, err := New(workspaceLookup, loader).Inspect(context.Background(), "ws_01K00000000000000000000000")
		if err != nil {
			t.Fatal(err)
		}
		if result.State == core.LoadAbsent && got.Status != core.StatusAbsent {
			t.Fatalf("absent=%#v", got)
		}
		if result.State == core.LoadInvalid && (got.Status != core.StatusInvalid || got.Code != core.CodeParseError) {
			t.Fatalf("invalid=%#v", got)
		}
	}
}

func TestProjectStatusInspectRejectsUnknownWorkspaceWithoutLoading(t *testing.T) {
	loader := &fakeLoader{}
	svc := New(fakeWorkspaceLookup{}, loader)
	_, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if !errors.Is(err, failure.WorkspaceNotFound) {
		t.Fatalf("err=%v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("loader called for unknown workspace: %d", loader.calls)
	}
}

func TestInspectionDoesNotExecuteManifestCommandThroughService(t *testing.T) {
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.never_run]\nshell=\"touch SENTINEL\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &fakeLoader{result: core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: "raw"}}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{{ID: "ws_01K00000000000000000000000", Root: "/repo"}}}, loader)
	got, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if err != nil || got.Manifest == nil || loader.calls != 1 {
		t.Fatalf("inspection=%#v err=%v calls=%d", got, err, loader.calls)
	}
}
