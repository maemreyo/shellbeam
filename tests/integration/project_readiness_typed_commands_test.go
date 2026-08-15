//go:build linux || darwin

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	projectcore "github.com/maemreyo/shellbeam/internal/core/project"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const a5NativeBindingManifest = `schema_version = 2

[commands.enum_cmd]
argv = ["tool", "{mode}"]
cwd = "."
[commands.enum_cmd.params.mode]
kind = "enum"
required = true
enum = ["-run", "safe"]

[commands.path_cmd]
argv = ["tool", "{path}"]
cwd = "."
[commands.path_cmd.params.path]
kind = "repo_path"
required = true
exists = "file"

[commands.package_cmd]
argv = ["go", "test", "{package}"]
cwd = "."
[commands.package_cmd.params.package]
kind = "repo_package"
provider = "go"
required = true

[commands.dep_setup]
argv = ["false"]
cwd = "."

[commands.dep_target]
argv = ["tool", "{name}"]
cwd = "."
depends_on = ["dep_setup"]
[commands.dep_target.params.name]
kind = "string"
required = true
`

type a5WorkspaceLookup struct {
	workspace workspacecore.Workspace
}

func (l a5WorkspaceLookup) ListWorkspaces(context.Context) ([]workspacecore.Workspace, error) {
	return []workspacecore.Workspace{l.workspace}, nil
}

type a5BindingHarness struct {
	root      string
	outside   string
	workspace workspacecore.Workspace
	binder    *projectapp.Binder
}

func TestProjectCommandNativeBindingPathPackageDependsOnAndP95(t *testing.T) {
	harness := newA5BindingHarness(t)
	assertA5EnumAndPathBinding(t, harness)
	assertA5PackageAndDependsOnBinding(t, harness)
	assertA5BindingP95(t, harness)
}

func newA5BindingHarness(t *testing.T) a5BindingHarness {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("inside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(root, ".shellbeam")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "project.toml"), []byte(a5NativeBindingManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	workspace := workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000",
		Label: "a5-native", Root: root, GitDir: filepath.Join(root, ".git"), CreatedAt: now, LastSeenAt: now,
	}
	binder := projectapp.NewBinder(a5WorkspaceLookup{workspace: workspace}, projectadapter.NewLoader(), projectadapter.NewRepoPathValidator(), projectadapter.NewGoPackageValidator())
	return a5BindingHarness{root: root, outside: outside, workspace: workspace, binder: binder}
}

func assertA5EnumAndPathBinding(t *testing.T, h a5BindingHarness) {
	t.Helper()
	ctx := context.Background()
	enumBinding, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "enum_cmd", Params: map[string]string{"mode": "-run"}})
	if err != nil || !reflect.DeepEqual(enumBinding.ResolvedArgv, []string{"tool", "-run"}) {
		t.Fatalf("option-like declared enum binding=%#v err=%v", enumBinding, err)
	}
	if _, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "path_cmd", Params: map[string]string{"path": "-flag"}}); err == nil {
		t.Fatal("leading-dash repo_path accepted")
	}
	if _, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "path_cmd", Params: map[string]string{"path": "escape"}}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	pathBinding, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "path_cmd", Params: map[string]string{"path": "inside.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if pathBinding.PathObservationQuality != projectcore.PathObservationExactAtBind || !reflect.DeepEqual(pathBinding.ResolvedArgv, []string{"tool", "inside.txt"}) {
		t.Fatalf("path binding=%#v", pathBinding)
	}
	inside := filepath.Join(h.root, "inside.txt")
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.outside, inside); err != nil {
		t.Fatal(err)
	}
	if err := pathBinding.Validate(); err != nil || !reflect.DeepEqual(pathBinding.ResolvedArgv, []string{"tool", "inside.txt"}) {
		t.Fatalf("post-bind path replacement mutated frozen binding: %#v err=%v", pathBinding, err)
	}
	if _, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "path_cmd", Params: map[string]string{"path": "inside.txt"}}); err == nil {
		t.Fatal("fresh bind after path replacement failed to observe symlink escape")
	}
}

func assertA5PackageAndDependsOnBinding(t *testing.T, h a5BindingHarness) {
	t.Helper()
	ctx := context.Background()
	packageBinding, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "package_cmd", Params: map[string]string{"package": "./pkg"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packageBinding.ResolvedArgv, []string{"go", "test", "./pkg"}) || packageBinding.Parameters[0].ProviderID != "go-repo-package" || packageBinding.Parameters[0].ProviderVersion != 1 {
		t.Fatalf("package binding=%#v", packageBinding)
	}
	depBinding, err := h.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "dep_target", Params: map[string]string{"name": "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(depBinding.ResolvedArgv, []string{"tool", "ok"}) {
		t.Fatalf("depends_on leaked into execution binding: %#v", depBinding)
	}
}

func assertA5BindingP95(t *testing.T, h a5BindingHarness) {
	t.Helper()
	ctx := context.Background()
	request := projectapp.BindRequest{WorkspaceID: string(h.workspace.ID), CommandID: "package_cmd", Params: map[string]string{"package": "./pkg"}}
	for i := 0; i < 20; i++ {
		if _, err := h.binder.Bind(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	durations := make([]time.Duration, 0, 200)
	for i := 0; i < 200; i++ {
		started := time.Now()
		if _, err := h.binder.Bind(ctx, request); err != nil {
			t.Fatal(err)
		}
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[189]
	t.Logf("typed project binding p95 over 200 warm native samples: %s", p95)
	if p95 > 10*time.Millisecond {
		t.Fatalf("typed project binding p95=%s exceeds 10ms native-host budget", p95)
	}
}
