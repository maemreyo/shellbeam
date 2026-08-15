package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	projectcore "github.com/maemreyo/shellbeam/internal/core/project"
	receiptcore "github.com/maemreyo/shellbeam/internal/core/receipt"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const a5AcceptanceManifestTemplate = `schema_version = 2

[toolchains.go]
version = "1.26"

[toolchains.node]
version = "22"

[requirements.toolchains.go]
required = true

[requirements.toolchains.node]
required = false

[requirements.executables.git]
required = true

[requirements.executables.a5-optional-missing]
required = false

[requirements.environment]
required_presence = ["SHELLBEAM_A5_PRESENT_SECRET", "SHELLBEAM_A5_ABSENT_7F33B5A4"]
optional_presence = ["SHELLBEAM_A5_OPTIONAL_ABSENT_7F33B5A4"]

[commands.never_run]
argv = ["touch", %q]
cwd = "."

[commands.test_package]
argv = ["go", "test", "{package}"]
cwd = "."

[commands.test_package.params.package]
kind = "repo_package"
provider = "go"
required = true
`

func TestProjectReadinessTypedProjectRealDaemonAcceptance(t *testing.T) {
	repo, manifestPath, commandSentinel, secret := prepareA5AcceptanceRepo(t)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	fresh := assertA5ReadinessAcceptance(t, client, workspace, commandSentinel, secret)
	assertA5RawStartAcceptance(t, client, workspace)
	assertA5TypedRetryAcceptance(t, client, workspace, manifestPath, fresh)
	if strings.Contains(string(readStateTree(t, stateDir)), secret) {
		t.Fatal("readiness persisted an environment value")
	}
}

func prepareA5AcceptanceRepo(t *testing.T) (string, string, string, string) {
	t.Helper()
	repo := initWorkspaceCLIRepo(t)
	writeTestFile(t, filepath.Join(repo, "go.mod"), "module example.com/a5accept\n\ngo 1.26.0\n")
	pkgDir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(pkgDir, "pkg.go"), "package pkg\n\nfunc Value() int { return 1 }\n")
	commandSentinel := filepath.Join(repo, "manifest-command-must-not-run")
	manifestPath := filepath.Join(repo, ".shellbeam", "project.toml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, manifestPath, fmt.Sprintf(a5AcceptanceManifestTemplate, commandSentinel))
	for _, name := range []string{"SHELLBEAM_A5_ABSENT_7F33B5A4", "SHELLBEAM_A5_OPTIONAL_ABSENT_7F33B5A4"} {
		if _, exists := os.LookupEnv(name); exists {
			t.Fatalf("acceptance missing-env sentinel %q unexpectedly exists in host environment", name)
		}
	}
	secret := "postgres://alice:super-secret@db/acceptance"
	t.Setenv("SHELLBEAM_A5_PRESENT_SECRET", secret)
	return repo, manifestPath, commandSentinel, secret
}

func attachA5AcceptanceWorkspace(t *testing.T, repo, stateDir string) workspacecore.Workspace {
	t.Helper()
	out, errOut, code := runWorkspaceCLI(t, "workspace", "attach", repo, "--label", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("workspace attach code=%d stderr=%q", code, errOut)
	}
	var workspace workspacecore.Workspace
	if err := json.Unmarshal([]byte(out), &workspace); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func assertA5ReadinessAcceptance(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, commandSentinel, secret string) projectcore.Readiness {
	t.Helper()
	fresh := inspectA5Readiness(t, client, string(workspace.ID), "readiness-fresh")
	if fresh.State != projectcore.ReadinessNotReady || fresh.CacheQuality != projectcore.CacheFresh || fresh.CacheAgeMS != 0 {
		t.Fatalf("fresh readiness=%#v", fresh)
	}
	checks := make(map[string]projectcore.ReadinessCheck, len(fresh.Checks))
	for _, check := range fresh.Checks {
		checks[string(check.Kind)+":"+check.ID] = check
	}
	requireA5ReadinessCheck(t, checks, "toolchain:go", projectcore.CheckCompatible, true)
	requireA5ReadinessCheck(t, checks, "toolchain:node", projectcore.CheckUnavailable, false)
	requireA5ReadinessCheck(t, checks, "executable:git", projectcore.CheckAvailable, true)
	requireA5ReadinessCheck(t, checks, "executable:a5-optional-missing", projectcore.CheckMissing, false)
	requireA5ReadinessCheck(t, checks, "environment_presence:SHELLBEAM_A5_PRESENT_SECRET", projectcore.CheckPresentNonEmpty, true)
	requireA5ReadinessCheck(t, checks, "environment_presence:SHELLBEAM_A5_ABSENT_7F33B5A4", projectcore.CheckAbsent, true)
	requireA5ReadinessCheck(t, checks, "environment_presence:SHELLBEAM_A5_OPTIONAL_ABSENT_7F33B5A4", projectcore.CheckAbsent, false)
	encoded, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("readiness leaked environment value: %s", encoded)
	}
	cached := inspectA5Readiness(t, client, string(workspace.ID), "readiness-cached")
	if cached.CacheQuality != projectcore.CacheCached || cached.CacheAgeMS < 0 || cached.ManifestDigest != fresh.ManifestDigest {
		t.Fatalf("cached readiness=%#v fresh=%#v", cached, fresh)
	}
	if _, err := os.Stat(commandSentinel); !os.IsNotExist(err) {
		t.Fatalf("manifest command ran during readiness inspection: %v", err)
	}
	return fresh
}

func requireA5ReadinessCheck(t *testing.T, checks map[string]projectcore.ReadinessCheck, key string, status projectcore.CheckStatus, required bool) {
	t.Helper()
	check, ok := checks[key]
	if !ok || check.Status != status || check.Required != required {
		t.Fatalf("readiness check %q=%#v found=%t want_status=%q required=%t", key, check, ok, status, required)
	}
}

func assertA5RawStartAcceptance(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace) {
	t.Helper()
	rawSentinel := filepath.Join(workspace.Root, "raw-start-ran")
	raw := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a5-real-raw", WorkspaceID: string(workspace.ID), CWD: ".",
		Argv: []string{"touch", rawSentinel},
	})
	assertA1ChildSuccess(t, raw)
	if _, err := os.Stat(rawSentinel); err != nil {
		t.Fatalf("raw arbitrary start was blocked by not_ready readiness: %v", err)
	}
}

func assertA5TypedRetryAcceptance(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, manifestPath string, fresh projectcore.Readiness) {
	t.Helper()
	request := ipcadapter.RequestV2{
		Action: "start", OperationID: "a5-real-typed", WorkspaceID: string(workspace.ID),
		ProjectCommandID: "test_package", Params: map[string]string{"package": "./pkg"},
	}
	first := callA1Terminal(t, client, request)
	assertA1ChildSuccess(t, first)
	if first.Receipt == nil || first.Receipt.SchemaVersion != 3 || first.Receipt.ProjectCommand == nil {
		t.Fatalf("typed receipt=%#v", first.Receipt)
	}
	binding := first.Receipt.ProjectCommand
	if !reflect.DeepEqual(binding.ResolvedArgv, []string{"go", "test", "./pkg"}) || binding.CommandID != "test_package" || binding.ManifestDigest != fresh.ManifestDigest {
		t.Fatalf("frozen binding=%#v", binding)
	}
	firstReceiptJSON, err := json.Marshal(first.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	firstBindingDigest, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(workspace.Root, "pkg")); err != nil {
		t.Fatal(err)
	}
	replayed := callA1Terminal(t, client, request)
	assertA5ReplayIdentity(t, first.Operation.SessionID, firstReceiptJSON, firstBindingDigest, replayed)
	conflict := request
	conflict.IPVersion, conflict.Kind, conflict.RequestID = 2, "request", "a5-real-typed-conflict"
	conflict.Params = map[string]string{"package": "./missing-provider-target"}
	response, err := client.CallV2(context.Background(), conflict)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Error == nil || response.Error.Code != "operation_conflict" {
		t.Fatalf("conflicting typed retry read current manifest/provider instead of caller fingerprint: %#v", response)
	}
}

func assertA5ReplayIdentity(t *testing.T, sessionID string, receiptJSON []byte, bindingDigest string, replayed receiptcore.Result) {
	t.Helper()
	if replayed.Operation.SessionID != sessionID || replayed.Receipt == nil || replayed.Receipt.ProjectCommand == nil {
		t.Fatalf("lost-response replay did not reuse admitted session: replay=%#v", replayed)
	}
	replayedJSON, err := json.Marshal(replayed.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(replayedJSON) != string(receiptJSON) {
		t.Fatalf("lost-response replay changed terminal receipt: first=%s replay=%s", receiptJSON, replayedJSON)
	}
	digest, err := replayed.Receipt.ProjectCommand.Digest()
	if err != nil || digest != bindingDigest {
		t.Fatalf("replayed binding digest=%q want=%q err=%v", digest, bindingDigest, err)
	}
}

func inspectA5Readiness(t *testing.T, client *ipcadapter.Client, workspaceID, requestID string) projectcore.Readiness {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: requestID, Action: "inspect.readiness", WorkspaceID: workspaceID,
	})
	if err != nil || !response.OK || response.Readiness == nil {
		if response.Error != nil {
			t.Fatalf("readiness response error=%#v transport=%v", *response.Error, err)
		}
		t.Fatalf("readiness response=%#v err=%v", response, err)
	}
	return *response.Readiness
}
