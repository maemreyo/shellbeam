//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const nativeHermeticWorkspaceID = "ws_01K00000000000000000000000"

type nativeHermeticWorkspace struct {
	root       string
	generation string
}

func (w nativeHermeticWorkspace) ResolveFresh(context.Context, string) (hermeticapp.WorkspaceContext, error) {
	return hermeticapp.WorkspaceContext{
		WorkspaceID:      workspacecore.WorkspaceID(nativeHermeticWorkspaceID),
		RepositoryID:     workspacecore.RepositoryID("repo_01K00000000000000000000000"),
		Root:             w.root,
		SourceGeneration: w.generation,
	}, nil
}

type nativeHermeticOwner struct {
	inner processadapter.Owner
	mu    sync.Mutex
	calls int
}

func (o *nativeHermeticOwner) Start(ctx context.Context, spec operation.ExecutionSpec, sink daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	return o.inner.Start(ctx, spec, sink)
}

func (o *nativeHermeticOwner) StartPrivateHermetic(ctx context.Context, command hermeticapp.ProviderCommand, sink daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error) {
	o.mu.Lock()
	o.calls++
	o.mu.Unlock()
	return o.inner.StartPrivateHermetic(ctx, command, sink)
}

func (o *nativeHermeticOwner) Calls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

type nativeHermeticSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
	err error
}

func (s *nativeHermeticSink) Append(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.buf.Write(data)
	return nil
}

func (s *nativeHermeticSink) CaptureFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *nativeHermeticSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type nativeHermeticMatrix struct {
	runtime    daemonapp.HermeticRuntime
	owner      *nativeHermeticOwner
	repoRoot   string
	runtimeDir string
}

func newNativeHermeticMatrix(t *testing.T) *nativeHermeticMatrix {
	t.Helper()
	if os.Getenv("HERMETIC_V1_NATIVE_REQUIRED") != "1" {
		t.Skip("native Hermetic V1 production matrix is CI-gated")
	}
	bwrapPath := os.Getenv("HERMETIC_V1_NATIVE_BWRAP")
	toolchainRoot := os.Getenv("HERMETIC_V1_NATIVE_TOOLCHAIN")
	if bwrapPath == "" || toolchainRoot == "" {
		t.Fatal("native Hermetic V1 provider fixture missing")
	}
	repoRoot, stateDir, runtimeDir := t.TempDir(), t.TempDir(), t.TempDir()
	mustWriteNativeFile(t, filepath.Join(repoRoot, "declared.txt"), []byte("declared-v1\n"), 0o644)
	mustWriteNativeFile(t, filepath.Join(repoRoot, "secret.txt"), []byte("TOP-SECRET-HOST\n"), 0o600)
	if err := os.Symlink("secret.txt", filepath.Join(repoRoot, "escape-link")); err != nil {
		t.Fatal(err)
	}
	writeNativeHermeticManifest(t, stateDir, bwrapPath, toolchainRoot, os.Getenv("HERMETIC_V1_NATIVE_SECURITY_POLICY"))
	workspace := nativeHermeticWorkspace{root: repoRoot, generation: "gen_" + strings.Repeat("d", 64)}
	owner := &nativeHermeticOwner{}
	runtimePort, catalog := composeHermeticBoundary(context.Background(), true, stateDir, runtimeDir, workspace, owner, capability.Baseline(capability.Limits{}), nil)
	if runtimePort == nil || catalog.Features[capability.FeatureHermeticBoundaryV1] != capability.Available || catalog.HermeticBoundary == nil {
		t.Fatalf("qualified native runtime was not advertised: runtime=%T support=%#v", runtimePort, catalog.HermeticBoundary)
	}
	return &nativeHermeticMatrix{runtime: runtimePort, owner: owner, repoRoot: repoRoot, runtimeDir: runtimeDir}
}

func TestHermeticV1NativeProductionMatrix(t *testing.T) {
	m := newNativeHermeticMatrix(t)
	t.Run("case_01_undeclared_repo_read_denied", m.case01UndeclaredRead)
	t.Run("case_02_escape_paths_and_symlink_denied", m.case02EscapePaths)
	t.Run("case_03_network_and_dns_denied", m.case03Network)
	t.Run("case_04_inherited_secret_environment_absent", m.case04Environment)
	t.Run("case_05_host_path_injection_impossible", m.case05HostPath)
	t.Run("case_06_descendant_tree_cannot_escape", m.case06Descendants)
	t.Run("case_07_post_capture_host_mutation_cannot_change_input", m.case07ImmutableCapture)
	t.Run("case_08_provider_kill_never_promotes_scope", m.case08KillLosesAuthority)
	t.Run("case_09_capture_budget_overflow_prevents_spawn", m.case09CaptureBudget)
	t.Run("case_10_sandbox_writes_do_not_mutate_host", m.case10WritesDiscarded)
	t.Run("case_11_only_successful_boundary_promotes_declared_scope", m.case11ScopePromotion)
	t.Run("case_13_repeated_production_runs_converge_private_state", m.case13CleanupConvergence)
}

func (m *nativeHermeticMatrix) case01UndeclaredRead(t *testing.T) {
	prepared, request := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeShell("cat secret.txt"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) == 0 || !result.Authoritative() {
		t.Fatalf("undeclared read exit=%#v boundary=%#v", exit, result)
	}
	assertNativeScope(t, prepared, request, result, true)
}

func (m *nativeHermeticMatrix) case02EscapePaths(t *testing.T) {
	before := m.owner.Calls()
	_, err := m.runtime.Prepare(context.Background(), daemonapp.HermeticPrepareRequest{
		WorkspaceID: nativeHermeticWorkspaceID, LogicalCWD: ".", Request: nativeRequest("escape-link"), Target: nativeArgv("cat", "escape-link"),
	})
	if err == nil {
		t.Fatal("symlink input was captured instead of rejected")
	}
	if m.owner.Calls() != before {
		t.Fatal("capture rejection spawned provider")
	}
	prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeShell("cat ../secret.txt >/dev/null 2>&1"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) == 0 || !result.Authoritative() {
		t.Fatalf("dotdot escape exit=%#v boundary=%#v", exit, result)
	}
}

func (m *nativeHermeticMatrix) case03Network(t *testing.T) {
	for name, target := range map[string]operation.ExecutionSpec{
		"connect": nativeArgv("curl", "-fsS", "--connect-timeout", "1", "--max-time", "2", "http://1.1.1.1/"),
		"dns":     nativeArgv("getent", "hosts", "example.com"),
	} {
		t.Run(name, func(t *testing.T) {
			prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, target)
			defer nativeDiscard(t, m.runtime, prepared)
			exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
			if nativeExitCode(exit) == 0 || !result.Authoritative() {
				t.Fatalf("network channel exit=%#v boundary=%#v", exit, result)
			}
		})
	}
}

func (m *nativeHermeticMatrix) case04Environment(t *testing.T) {
	t.Setenv("TOP_SECRET", "must-not-cross")
	prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeArgv("env"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, output := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) != 0 || !result.Authoritative() {
		t.Fatalf("env exit=%#v boundary=%#v", exit, result)
	}
	got := nativeEnvMap(t, output)
	want := map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/homeless", "PWD": "/work/input", "LANG": "C.UTF-8"}
	if len(got) != len(want) {
		t.Fatalf("unexpected environment=%v", got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("environment %s=%q want=%q all=%v", key, got[key], value, got)
		}
	}
	if _, exists := got["TOP_SECRET"]; exists {
		t.Fatal("ambient secret crossed Hermetic boundary")
	}
}

func (m *nativeHermeticMatrix) case05HostPath(t *testing.T) {
	prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeShell("command -v eviltool >/dev/null 2>&1"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) == 0 || !result.Authoritative() {
		t.Fatalf("host PATH injection exit=%#v boundary=%#v", exit, result)
	}
}

func (m *nativeHermeticMatrix) case06Descendants(t *testing.T) {
	command := `if cat secret.txt >/dev/null 2>&1; then exit 90; fi; (if cat secret.txt >/dev/null 2>&1; then exit 91; fi) & wait`
	prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeShell(command))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) != 0 || !result.Authoritative() {
		t.Fatalf("descendant escape exit=%#v boundary=%#v", exit, result)
	}
}

func (m *nativeHermeticMatrix) case07ImmutableCapture(t *testing.T) {
	path := filepath.Join(m.repoRoot, "declared.txt")
	mustWriteNativeFile(t, path, []byte("declared-v1\n"), 0o644)
	prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeArgv("cat", "declared.txt"))
	defer nativeDiscard(t, m.runtime, prepared)
	mustWriteNativeFile(t, path, []byte("declared-v2-host\n"), 0o644)
	exit, result, output := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) != 0 || !result.Authoritative() || output != "declared-v1\n" {
		t.Fatalf("immutable capture exit=%#v boundary=%#v output=%q", exit, result, output)
	}
	host, err := os.ReadFile(path)
	if err != nil || string(host) != "declared-v2-host\n" {
		t.Fatalf("host mutation truth=%q err=%v", host, err)
	}
}

func (m *nativeHermeticMatrix) case08KillLosesAuthority(t *testing.T) {
	prepared, request := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeArgv("sleep", "60"))
	defer nativeDiscard(t, m.runtime, prepared)
	handle, spawn, err := m.runtime.Start(context.Background(), prepared, &nativeHermeticSink{})
	if err != nil || handle == nil || !spawn.Succeeded {
		t.Fatalf("native provider did not start: spawn=%#v err=%v", spawn, err)
	}
	signal := handle.Signal("KILL")
	if !signal.Attempted || !signal.Succeeded {
		t.Fatalf("provider kill failed: %#v", signal)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = handle.Wait(ctx)
	result := nativeBoundaryResult(t, handle)
	if result.Authoritative() || result.Continuity != hermeticcore.ContinuityLost || !result.EstablishedPreExec {
		t.Fatalf("provider kill retained authority: %#v", result)
	}
	assertNativeScope(t, prepared, request, result, false)
}

func (m *nativeHermeticMatrix) case09CaptureBudget(t *testing.T) {
	oversized := filepath.Join(m.repoRoot, "oversized.bin")
	file, err := os.OpenFile(oversized, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(hermeticcore.MaxCaptureFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before := m.owner.Calls()
	_, err = m.runtime.Prepare(context.Background(), daemonapp.HermeticPrepareRequest{
		WorkspaceID: nativeHermeticWorkspaceID, LogicalCWD: ".", Request: nativeRequest("oversized.bin"), Target: nativeArgv("true"),
	})
	if err == nil {
		t.Fatal("oversized capture was accepted")
	}
	if m.owner.Calls() != before {
		t.Fatalf("capture overflow spawned provider calls before=%d after=%d", before, m.owner.Calls())
	}
	if err := os.Remove(oversized); err != nil {
		t.Fatal(err)
	}
}

func (m *nativeHermeticMatrix) case10WritesDiscarded(t *testing.T) {
	path := filepath.Join(m.repoRoot, "writeguard.txt")
	mustWriteNativeFile(t, path, []byte("host-original\n"), 0o644)
	prepared, _ := nativePrepare(t, m.runtime, []string{"writeguard.txt"}, nativeShell("printf changed > writeguard.txt"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) == 0 || !result.Authoritative() {
		t.Fatalf("captured write exit=%#v boundary=%#v", exit, result)
	}
	host, err := os.ReadFile(path)
	if err != nil || string(host) != "host-original\n" {
		t.Fatalf("host worktree mutated=%q err=%v", host, err)
	}
	preparedScratch, _ := nativePrepare(t, m.runtime, []string{"writeguard.txt"}, nativeShell("printf private > /work/scratch/out && cat /work/scratch/out"))
	defer nativeDiscard(t, m.runtime, preparedScratch)
	exit, result, output := nativeRunPrepared(t, m.runtime, preparedScratch)
	if nativeExitCode(exit) != 0 || !result.Authoritative() || output != "private" {
		t.Fatalf("ephemeral scratch exit=%#v boundary=%#v output=%q", exit, result, output)
	}
}

func (m *nativeHermeticMatrix) case11ScopePromotion(t *testing.T) {
	prepared, request := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeArgv("true"))
	defer nativeDiscard(t, m.runtime, prepared)
	exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
	if nativeExitCode(exit) != 0 || !result.Authoritative() {
		t.Fatalf("successful boundary exit=%#v result=%#v", exit, result)
	}
	scope := assertNativeScope(t, prepared, request, result, true)
	if len(scope.RepoInputs) != 1 || scope.RepoInputs[0] != "declared.txt" {
		t.Fatalf("scope widened beyond declared input: %#v", scope.RepoInputs)
	}
}

func (m *nativeHermeticMatrix) case13CleanupConvergence(t *testing.T) {
	for i := 0; i < 10; i++ {
		prepared, _ := nativePrepare(t, m.runtime, []string{"declared.txt"}, nativeArgv("true"))
		exit, result, _ := nativeRunPrepared(t, m.runtime, prepared)
		if nativeExitCode(exit) != 0 || !result.Authoritative() {
			t.Fatalf("iteration %d exit=%#v boundary=%#v", i, exit, result)
		}
		nativeDiscard(t, m.runtime, prepared)
	}
	for _, rel := range []string{"captures", "boundaries"} {
		entries, err := os.ReadDir(filepath.Join(m.runtimeDir, "hermetic-v1", rel))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("private %s residue=%v", rel, entries)
		}
	}
}

func nativeRequest(inputs ...string) hermeticcore.Request {
	return hermeticcore.Request{
		Version: hermeticcore.RequestVersionV1, Mode: hermeticcore.ModeRequired, RepoInputs: append([]string(nil), inputs...),
		Network: hermeticcore.NetworkOff, Environment: hermeticcore.EnvironmentFixedAllowlist, Stdin: hermeticcore.StdinClosed, Writes: hermeticcore.WritesEphemeralDiscard,
	}
}

func nativeShell(command string) operation.ExecutionSpec {
	return operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: command, StdinMode: operation.StdinModeClosed}
}

func nativeArgv(argv ...string) operation.ExecutionSpec {
	return operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: append([]string(nil), argv...), StdinMode: operation.StdinModeClosed}
}

func nativePrepare(t *testing.T, runtimePort daemonapp.HermeticRuntime, inputs []string, target operation.ExecutionSpec) (hermeticapp.PreparedExecution, hermeticcore.Request) {
	t.Helper()
	request := nativeRequest(inputs...)
	prepared, err := runtimePort.Prepare(context.Background(), daemonapp.HermeticPrepareRequest{
		WorkspaceID: nativeHermeticWorkspaceID, LogicalCWD: ".", Request: request, Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	return prepared, request
}

func nativeRunPrepared(t *testing.T, runtimePort daemonapp.HermeticRuntime, prepared hermeticapp.PreparedExecution) (receipt.ExitEvidence, hermeticcore.BoundaryResult, string) {
	t.Helper()
	sink := &nativeHermeticSink{}
	handle, spawn, err := runtimePort.Start(context.Background(), prepared, sink)
	if err != nil || handle == nil || !spawn.Succeeded {
		t.Fatalf("provider start spawn=%#v err=%v", spawn, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exit := handle.Wait(ctx)
	if sink.err != nil {
		t.Fatalf("output capture failed: %v", sink.err)
	}
	return exit, nativeBoundaryResult(t, handle), sink.String()
}

func nativeBoundaryResult(t *testing.T, handle daemonapp.ProcessHandle) hermeticcore.BoundaryResult {
	t.Helper()
	aware, ok := handle.(interface {
		HermeticBoundaryResult() hermeticcore.BoundaryResult
	})
	if !ok {
		t.Fatalf("native handle lacks Hermetic boundary truth: %T", handle)
	}
	return aware.HermeticBoundaryResult()
}

func nativeDiscard(t *testing.T, runtimePort daemonapp.HermeticRuntime, prepared hermeticapp.PreparedExecution) {
	t.Helper()
	if err := runtimePort.Discard(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
}

func nativeExitCode(exit receipt.ExitEvidence) int {
	if exit.Code == nil {
		return -1
	}
	return *exit.Code
}

func assertNativeScope(t *testing.T, prepared hermeticapp.PreparedExecution, request hermeticcore.Request, result hermeticcore.BoundaryResult, want bool) hermeticcore.ProvenInputScope {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	binding := hermeticcore.BoundaryBinding{
		SchemaVersion: hermeticcore.BoundaryBindingSchemaV1, BoundaryID: prepared.BoundaryID, Request: canonical,
		CaptureManifestSHA256: prepared.CaptureManifestSHA256, CaptureContentSHA256: prepared.CaptureContentSHA256,
		Provider: prepared.Provider, Toolchain: prepared.Toolchain,
	}
	scope, ok, err := hermeticcore.ProvenInputScopeFromCompletion(binding, result)
	if err != nil {
		t.Fatal(err)
	}
	if ok != want {
		t.Fatalf("scope promotion=%v want=%v boundary=%#v", ok, want, result)
	}
	return scope
}

func nativeEnvMap(t *testing.T, output string) map[string]string {
	t.Helper()
	got := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			t.Fatalf("invalid environment output line %q", line)
		}
		got[key] = value
	}
	return got
}

func writeNativeHermeticManifest(t *testing.T, stateDir, bwrapPath, toolchainRoot, securityPolicyPath string) {
	t.Helper()
	provider := hermeticcore.ProviderIdentity{
		Provider: hermeticcore.ProviderBubblewrap, Version: hermeticcore.BubblewrapVersionV1,
		BinarySHA256: nativeFileSHA256(t, bwrapPath), RuntimeManifestSHA256: nativeRuntimeManifestSHA256(t, bwrapPath),
	}
	if securityPolicyPath != "" {
		provider.SecurityPolicyID = "apparmor-bwrap-userns-restrict"
		provider.SecurityPolicySHA256 = nativeFileSHA256(t, securityPolicyPath)
	}
	manifest := hermeticProviderManifest{
		SchemaVersion: 1, BubblewrapPath: bwrapPath, ToolchainRoot: toolchainRoot,
		Provider: provider, Toolchain: hermeticcore.ToolchainIdentity{ID: "task7-linux-" + runtime.GOARCH, ManifestSHA256: nativeToolchainManifestSHA256(t, toolchainRoot)},
		SecurityPolicyPath: securityPolicyPath,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(stateDir, "hermetic-v1")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func nativeRuntimeManifestSHA256(t *testing.T, bwrapPath string) string {
	t.Helper()
	ldd := "/usr/bin/ldd"
	if _, err := os.Stat(ldd); err != nil {
		ldd = "/bin/ldd"
	}
	cmd := exec.Command(ldd, bwrapPath)
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]struct{}{}
	for _, field := range strings.Fields(string(out)) {
		if strings.HasPrefix(field, "/") {
			set[filepath.Clean(strings.TrimRight(field, ",;"))] = struct{}{}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var manifest strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&manifest, "%s  %s\n", nativeFileSHA256(t, path), path)
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:])
}

func nativeToolchainManifestSHA256(t *testing.T, root string) string {
	t.Helper()
	lines := make([]string, 0, 256)
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			lines = append(lines, fmt.Sprintf("D %04o %s\n", mode.Perm(), rel))
		case mode.IsRegular():
			lines = append(lines, fmt.Sprintf("F %04o %s %s\n", mode.Perm(), rel, nativeFileSHA256(t, path)))
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			lines = append(lines, fmt.Sprintf("L %s -> %s\n", rel, target))
		default:
			return fmt.Errorf("unsupported toolchain entry %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:])
}

func nativeFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustWriteNativeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
