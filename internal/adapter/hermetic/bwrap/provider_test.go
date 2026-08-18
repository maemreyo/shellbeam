package bwrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestPrepareBuildsFrozenTopologyAndExactEnvironment(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Prepare(context.Background(), fixture.request(operation.ExecutionSpec{
		Mode:      operation.ExecutionModeShell,
		Shell:     "/host/ignored-shell",
		Command:   "go test ./...",
		CWD:       "/host/worktree",
		StdinMode: operation.StdinModeClosed,
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{
		fixture.config.BubblewrapPath,
		"--unshare-user", "--unshare-all", "--die-with-parent", "--disable-userns", "--assert-userns-disabled",
		"--json-status-fd", "3",
		"--ro-bind", fixture.config.ToolchainRoot, "/",
		"--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", fixture.capture.PrivateRoot, "/work/input",
		"--bind", got.ScratchRoot, "/work/scratch",
		"--clearenv",
		"--setenv", "PATH", "/usr/bin:/bin",
		"--setenv", "HOME", "/homeless",
		"--setenv", "PWD", "/work/input",
		"--setenv", "LANG", "C.UTF-8",
		"--chdir", "/work/input", "--", "/bin/sh", "-lc", "go test ./...",
	}
	if !reflect.DeepEqual(got.Command.Argv, wantPrefix) {
		t.Fatalf("provider argv mismatch\n got=%q\nwant=%q", got.Command.Argv, wantPrefix)
	}
	if got.Command.Executable != fixture.config.BubblewrapPath || got.Command.Dir != "/" || len(got.Command.Env) != 0 || got.Command.StdinMode != operation.StdinModeClosed {
		t.Fatalf("provider command=%#v", got.Command)
	}
	wantContentDigest, err := fixture.capture.Manifest.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != fixture.config.ProviderIdentity || got.Toolchain != fixture.config.ToolchainIdentity || got.CaptureManifestSHA256 == "" || got.CaptureContentSHA256 != wantContentDigest {
		t.Fatalf("prepared identities=%#v", got)
	}
	if got.BoundaryID == "" || got.PrivateStateRoot == "" || got.ScratchRoot == "" {
		t.Fatalf("missing private execution identity: %#v", got)
	}
}

func TestPrepareAddsPrivateJSONStatusFDForPreExecAndContinuityProof(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.Prepare(context.Background(), fixture.request(operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed}))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Command.StatusFD != 3 {
		t.Fatalf("status fd=%d, want 3", prepared.Command.StatusFD)
	}
	for i := 0; i+1 < len(prepared.Command.Argv); i++ {
		if prepared.Command.Argv[i] == "--json-status-fd" && prepared.Command.Argv[i+1] == "3" {
			return
		}
	}
	t.Fatalf("provider argv missing json status fd: %v", prepared.Command.Argv)
}

func TestPrepareResolvesBareArgvOnlyAgainstFixedToolchainPath(t *testing.T) {
	fixture := newProviderFixture(t)
	fixture.ops.executables["/usr/bin/go"] = true
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Prepare(context.Background(), fixture.request(operation.ExecutionSpec{
		Mode:      operation.ExecutionModeArgv,
		Argv:      []string{"go", "test", "./..."},
		CWD:       "/host/worktree",
		StdinMode: operation.StdinModeClosed,
	}))
	if err != nil {
		t.Fatal(err)
	}
	tail := got.Command.Argv[len(got.Command.Argv)-4:]
	if !reflect.DeepEqual(tail, []string{"--", "/usr/bin/go", "test", "./..."}) {
		t.Fatalf("target argv=%q", tail)
	}
	if fixture.ops.lookPathCalls != 0 {
		t.Fatal("provider used ambient host PATH")
	}
}

func TestPrepareRejectsAmbientTraceEnvTTYOrOpenStdinBeforePrivateState(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	cases := []operation.ExecutionSpec{
		{Mode: operation.ExecutionModeShell, Command: "true", TTY: true, StdinMode: operation.StdinModeClosed},
		{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeStream},
		{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed, EnvironmentAdditions: []operation.EnvironmentEntry{{Key: "SHELLBEAM_TRACE_ID", Value: "trace"}}},
	}
	for i, target := range cases {
		if _, err := provider.Prepare(context.Background(), fixture.request(target)); err == nil {
			t.Fatalf("case %d accepted incompatible target", i)
		}
	}
	entries, err := os.ReadDir(fixture.config.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused prepare left private state: %v", entries)
	}
}

func TestProviderQualificationFailurePreventsAnyPrepareState(t *testing.T) {
	fixture := newProviderFixture(t)
	fixture.ops.qualifyErr = errors.New("identity drift")
	if _, err := newWithOps(context.Background(), fixture.config, fixture.ops); err == nil {
		t.Fatal("provider qualification drift accepted")
	}
	entries, err := os.ReadDir(fixture.config.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("qualification failure created state: %v", entries)
	}
}

func TestDiscardOnlyRemovesOwnedBoundaryState(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.Prepare(context.Background(), fixture.request(operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed}))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Discard(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.PrivateStateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private state survived discard: %v", err)
	}
	forged := prepared
	forged.PrivateStateRoot = filepath.Join(fixture.config.RuntimeRoot, "other")
	if err := provider.Discard(context.Background(), forged); err == nil {
		t.Fatal("forged private state ownership accepted")
	}
}

type fakeOps struct {
	provider      core.ProviderIdentity
	toolchain     core.ToolchainIdentity
	executables   map[string]bool
	qualifyErr    error
	lookPathCalls int
}

func (o *fakeOps) Qualify(context.Context, Config) (core.ProviderIdentity, core.ToolchainIdentity, error) {
	if o.qualifyErr != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, o.qualifyErr
	}
	return o.provider, o.toolchain, nil
}
func (o *fakeOps) ToolchainExecutable(_ string, sandboxPath string) bool {
	return o.executables[sandboxPath]
}

type providerFixture struct {
	config  Config
	ops     *fakeOps
	capture app.CapturedView
}

func newProviderFixture(t *testing.T) providerFixture {
	t.Helper()
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "runtime")
	toolchainRoot := filepath.Join(root, "toolchain")
	captureRoot := filepath.Join(root, "capture")
	for _, dir := range []string{runtimeRoot, toolchainRoot, captureRoot} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	providerID := core.ProviderIdentity{Provider: core.ProviderBubblewrap, Version: core.BubblewrapVersionV1, BinarySHA256: hex64('a'), RuntimeManifestSHA256: hex64('b')}
	toolchainID := core.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: hex64('c')}
	cfg := Config{BubblewrapPath: "/qualified/bwrap", ToolchainRoot: toolchainRoot, RuntimeRoot: runtimeRoot, ProviderIdentity: providerID, ToolchainIdentity: toolchainID}
	manifest := core.CaptureManifest{SchemaVersion: core.CaptureManifestSchemaVersion, WorkspaceID: workspacecore.WorkspaceID("ws_01K00000000000000000000000"), SourceGeneration: "gen_" + hex64('d'), Selectors: []string{"go.mod"}, Entries: []core.CaptureEntry{{Path: "go.mod", Size: 1, SHA256: hex64('e')}}, TotalBytes: 1}
	manifest, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	capture := app.CapturedView{CaptureID: "hcap_01K00000000000000000000000", PrivateRoot: captureRoot, Manifest: manifest}
	ops := &fakeOps{provider: providerID, toolchain: toolchainID, executables: map[string]bool{"/bin/sh": true}}
	return providerFixture{config: cfg, ops: ops, capture: capture}
}
func (f providerFixture) request(target operation.ExecutionSpec) app.PrepareExecutionRequest {
	return app.PrepareExecutionRequest{LogicalCWD: ".", Request: core.Request{Version: 1, Mode: core.ModeRequired, RepoInputs: []string{"go.mod"}, Network: core.NetworkOff, Environment: core.EnvironmentFixedAllowlist, Stdin: core.StdinClosed, Writes: core.WritesEphemeralDiscard}, Capture: f.capture, Target: target}
}
func hex64(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}

func TestPrepareRequalifiesIdentityAndFailsClosedOnPostStartupDrift(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	fixture.ops.provider.BinarySHA256 = hex64('f')
	if _, err := provider.Prepare(context.Background(), fixture.request(operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed})); err == nil {
		t.Fatal("post-startup provider identity drift accepted")
	}
	entries, err := os.ReadDir(fixture.config.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("identity drift created private execution state: %v", entries)
	}
}

func TestPrepareMapsLogicalCWDInsideImmutableInputViewAndRejectsEscape(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.capture.PrivateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fixture.capture.PrivateRoot, "internal/app"), 0o755); err != nil {
		t.Fatal(err)
	}
	req := fixture.request(operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed})
	req.LogicalCWD = "internal/app"
	got, err := provider.Prepare(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want := "/work/input/internal/app"
	var pwd, chdir string
	for i := 0; i < len(got.Command.Argv); i++ {
		if got.Command.Argv[i] == "--setenv" && i+2 < len(got.Command.Argv) && got.Command.Argv[i+1] == "PWD" {
			pwd = got.Command.Argv[i+2]
		}
		if got.Command.Argv[i] == "--chdir" && i+1 < len(got.Command.Argv) {
			chdir = got.Command.Argv[i+1]
		}
	}
	if pwd != want || chdir != want {
		t.Fatalf("pwd=%q chdir=%q want=%q argv=%q", pwd, chdir, want, got.Command.Argv)
	}
	for _, bad := range []string{"../outside", "/absolute", "internal/../../outside"} {
		req := fixture.request(operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", StdinMode: operation.StdinModeClosed})
		req.LogicalCWD = bad
		if _, err := provider.Prepare(context.Background(), req); err == nil {
			t.Fatalf("logical cwd escape %q accepted", bad)
		}
	}
}

func TestSweepRemovesOnlyStaleOwnedBoundaryDirectoriesAndIsIdempotent(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(fixture.config.RuntimeRoot, "hb_01K00000000000000000000000")
	if err := os.MkdirAll(filepath.Join(stale, "scratch", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "scratch", "nested", "x"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(fixture.config.RuntimeRoot, "keep-me")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := provider.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale owned boundary survived: %v", err)
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Fatalf("foreign sibling was removed: %v", err)
	}
	if err := provider.Sweep(context.Background()); err != nil {
		t.Fatalf("idempotent sweep failed: %v", err)
	}
}

func TestSweepFailsClosedOnOwnedLookingBoundarySymlink(t *testing.T) {
	fixture := newProviderFixture(t)
	provider, err := newWithOps(context.Background(), fixture.config, fixture.ops)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(target, []byte("do-not-touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(fixture.config.RuntimeRoot, "hb_01K00000000000000000000000")
	if err := os.Symlink(target, entry); err != nil {
		t.Fatal(err)
	}
	if err := provider.Sweep(context.Background()); err == nil {
		t.Fatal("owned-looking symlink accepted")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "do-not-touch" {
		t.Fatalf("target changed data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(entry); err != nil {
		t.Fatalf("ambiguous entry was modified: %v", err)
	}
}
