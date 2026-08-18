//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestA25RealDaemonEnvironmentAcceptance(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	repo := prepareA25EnvironmentRepo(t)
	store := openA1Store(t, stateDir)
	workspace, err := workspaceapp.New(store, gitadapter.New()).Attach(context.Background(), repo, "a25-env")
	if err != nil {
		t.Fatal(err)
	}
	secret := "postgres://alice:acceptance-secret@db/a25"
	lowEntropy := "a25-low-entropy-value"
	t.Setenv("SHELLBEAM_A25_SECRET", secret)
	t.Setenv("SHELLBEAM_A25_LOW", lowEntropy)
	rawPath := os.Getenv("PATH")
	client := runA1Daemon(t, stateDir, runtimeDir)

	assertA25ServerCatalog(t, client)
	first := inspectA25Environment(t, client, string(workspace.ID), environmentcore.FreshnessRefresh, "env-first")
	assertA25EnvironmentFacts(t, first, secret, lowEntropy, rawPath)
	cached := inspectA25Environment(t, client, string(workspace.ID), environmentcore.FreshnessCached, "env-cached")
	if cached.SnapshotID != first.SnapshotID {
		t.Fatalf("cached snapshot changed: %q -> %q", first.SnapshotID, cached.SnapshotID)
	}
	refreshed := inspectA25Environment(t, client, string(workspace.ID), environmentcore.FreshnessRefresh, "env-refresh")
	if refreshed.SnapshotID == first.SnapshotID {
		t.Fatal("refresh reused snapshot identity")
	}
	if refreshed.EnvironmentFingerprint != first.EnvironmentFingerprint || refreshed.ToolchainFingerprint != first.ToolchainFingerprint {
		t.Fatalf("equivalent refresh changed fingerprints env=%q/%q tool=%q/%q", first.EnvironmentFingerprint, refreshed.EnvironmentFingerprint, first.ToolchainFingerprint, refreshed.ToolchainFingerprint)
	}
}

func TestA25RealDaemonProcessSessionPIDPortsAndRestart(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	lsofLog := filepath.Join(t.TempDir(), "lsof.log")
	prependA25FakeExecutable(t, "lsof", lsofLog)
	client, stop := startA25Daemon(t, stateDir, runtimeDir)
	started := startA25LongProcess(t, client)

	bySession := waitA25ProcessDescendant(t, client, started.Operation.SessionID)
	if bySession.Root == nil || bySession.Root.Relation != processcore.RelationShellBeamRoot || len(bySession.Descendants) > processcore.MaxDescendants {
		t.Fatalf("session observation=%#v", bySession)
	}
	if _, err := os.Stat(lsofLog); !os.IsNotExist(err) {
		t.Fatalf("include_ports=false invoked lsof: %v", err)
	}
	byPID := inspectA25Process(t, client, processcore.Target{Kind: processcore.TargetPID, PID: bySession.Root.PID}, false, "proc-pid")
	if byPID.Root == nil || byPID.Root.PID != bySession.Root.PID || byPID.Root.Relation != processcore.RelationExternal {
		t.Fatalf("pid observation=%#v", byPID)
	}
	withPorts := inspectA25Process(t, client, processcore.Target{Kind: processcore.TargetSession, SessionID: started.Operation.SessionID}, true, "proc-ports")
	if withPorts.Root == nil || withPorts.Quality != processcore.QualityPartial || !a25ContainsDiagnostic(withPorts.DiagnosticCodes, processcore.DiagnosticPortUnavailable) {
		t.Fatalf("port-failure observation=%#v", withPorts)
	}
	if _, err := os.Stat(lsofLog); err != nil {
		t.Fatalf("include_ports=true did not invoke lsof: %v", err)
	}

	killA25Session(t, client, started.Operation.SessionID)
	terminal := inspectA25Process(t, client, processcore.Target{Kind: processcore.TargetSession, SessionID: started.Operation.SessionID}, false, "proc-terminal")
	assertA25UnavailableSession(t, terminal)
	stop()
	restarted, stopRestart := startA25Daemon(t, stateDir, runtimeDir)
	defer stopRestart()
	afterRestart := inspectA25Process(t, restarted, processcore.Target{Kind: processcore.TargetSession, SessionID: started.Operation.SessionID}, false, "proc-restart")
	assertA25UnavailableSession(t, afterRestart)
}

func TestA25RealDaemonOrdinaryExecutionPaysNoObservationTax(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	probeLog := filepath.Join(t.TempDir(), "probe.log")
	fakeBin := filepath.Join(t.TempDir(), "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go", "node", "python3", "java", "rustc", "ps", "lsof"} {
		writeA25ProbeExecutable(t, fakeBin, name, probeLog)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	client := runA1Daemon(t, stateDir, runtimeDir)
	result := callA1Terminal(t, client, ipcadapter.RequestV2{Action: "start", OperationID: "a25-no-tax", CWD: t.TempDir(), Command: "true"})
	assertA1ChildSuccess(t, result)
	if data, err := os.ReadFile(probeLog); err == nil && len(data) > 0 {
		t.Fatalf("ordinary execution invoked A2.5 probe/enumerator: %q", data)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	state := string(readA25StateTree(t, stateDir))
	for _, forbidden := range []string{"environment_fingerprint", "snapshot_id", "toolchain_fingerprint"} {
		if strings.Contains(state, forbidden) {
			t.Fatalf("ordinary execution persisted A2.5 snapshot field %q", forbidden)
		}
	}
}

func prepareA25EnvironmentRepo(t *testing.T) string {
	t.Helper()
	repo := initWorkspaceCLIRepo(t)
	dir := filepath.Join(repo, ".shellbeam")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `schema_version = 2

[environment]
relevant_presence = ["SHELLBEAM_A25_SECRET", "SHELLBEAM_A25_LOW"]

[toolchains.go]
version = "host"
`
	if err := os.WriteFile(filepath.Join(dir, "project.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func assertA25ServerCatalog(t *testing.T, client *ipcadapter.Client) {
	t.Helper()
	response := callA25V2(t, client, ipcadapter.RequestV2{Action: "inspect.server", RequestID: "a25-server"})
	if response.Server == nil {
		t.Fatal("inspect.server missing catalog")
	}
	catalog := response.Server
	if catalog.Features[capability.FeatureEnvironmentFingerprint] != capability.Available || catalog.Features[capability.FeatureProcessInspection] != capability.Available {
		t.Fatalf("A2.5 features=%#v", catalog.Features)
	}
	if !equalA25Ints(catalog.EnvironmentSnapshotSchemaVersions, []int{1}) || !equalA25Ints(catalog.EnvironmentFingerprintVersions, []int{1}) || !equalA25Ints(catalog.ToolchainFingerprintVersions, []int{1}) || !equalA25Ints(catalog.ProcessObservationSchemaVersions, []int{1}) {
		t.Fatalf("A2.5 schema versions=%#v", catalog)
	}
	if strings.Join(catalog.EnvironmentToolchainProbeIDs, ",") != "go,node,python,java,rust" || !catalog.PortObservationSupported {
		t.Fatalf("A2.5 probe/port support=%#v", catalog)
	}
	limits := catalog.Limits
	if limits.EnvironmentRelevantVariables != 64 || limits.EnvironmentToolchainProbes != 5 || limits.EnvironmentToolchainObservations != 16 || limits.EnvironmentProbeTimeoutMS != 2000 || limits.EnvironmentProbeOutputBytes != 512 || limits.EnvironmentCacheEntries != 128 || limits.ProcessDescendants != 128 || limits.ProcessTraversalDepth != 8 || limits.ProcessObservationBytes != 64<<10 || limits.ProcessObservationMS != 2000 || limits.ProcessPortRecords != 64 {
		t.Fatalf("A2.5 limits=%#v", limits)
	}
}

func inspectA25Environment(t *testing.T, client *ipcadapter.Client, workspaceID string, freshness environmentcore.Freshness, requestID string) environmentcore.Snapshot {
	t.Helper()
	response := callA25V2(t, client, ipcadapter.RequestV2{Action: "inspect.environment", RequestID: requestID, WorkspaceID: workspaceID, Freshness: freshness})
	if response.Environment == nil {
		t.Fatal("inspect.environment missing snapshot")
	}
	if err := response.Environment.Validate(); err != nil {
		t.Fatalf("invalid environment snapshot: %v", err)
	}
	return *response.Environment
}

func assertA25EnvironmentFacts(t *testing.T, snapshot environmentcore.Snapshot, secret, lowEntropy, rawPath string) {
	t.Helper()
	if snapshot.Platform.OS != runtime.GOOS || snapshot.Platform.Architecture != runtime.GOARCH || snapshot.Path.Digest == "" || snapshot.Path.EntryCount < 1 || snapshot.EnvironmentFingerprint == "" || snapshot.ToolchainFingerprint == "" {
		t.Fatalf("environment facts=%#v", snapshot)
	}
	presence := map[string]bool{}
	for _, fact := range snapshot.VariablePresence {
		presence[fact.Name] = fact.Present
	}
	if !presence["SHELLBEAM_A25_SECRET"] || !presence["SHELLBEAM_A25_LOW"] {
		t.Fatalf("presence=%#v", presence)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{rawPath, secret, lowEntropy, a25SHA256(secret), a25SHA256(lowEntropy)} {
		if forbidden != "" && strings.Contains(text, forbidden) {
			t.Fatalf("environment JSON leaked forbidden value %q: %s", forbidden, text)
		}
	}
}

func startA25LongProcess(t *testing.T, client *ipcadapter.Client) receipt.Result {
	t.Helper()
	response := callA25V2(t, client, ipcadapter.RequestV2{Action: "start", RequestID: "a25-long", OperationID: "a25-long", CWD: t.TempDir(), Command: "sleep 30 & wait", YieldMS: 20, MaxOutputBytes: 4096})
	if response.Result == nil || response.Result.Operation.SessionID == "" || response.Result.Operation.State == receipt.OperationTerminal {
		t.Fatalf("long process start=%#v", response.Result)
	}
	return *response.Result
}

func waitA25ProcessDescendant(t *testing.T, client *ipcadapter.Client, sessionID string) processcore.Observation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		observation := inspectA25Process(t, client, processcore.Target{Kind: processcore.TargetSession, SessionID: sessionID}, false, "proc-session-"+string(rune('a'+attempt%26)))
		if observation.Root != nil && len(observation.Descendants) > 0 {
			return observation
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("process descendant did not become observable")
	return processcore.Observation{}
}

func inspectA25Process(t *testing.T, client *ipcadapter.Client, target processcore.Target, includePorts bool, requestID string) processcore.Observation {
	t.Helper()
	response := callA25V2(t, client, ipcadapter.RequestV2{Action: "inspect.process", RequestID: requestID, ProcessTarget: &target, IncludePorts: includePorts})
	if response.Process == nil {
		t.Fatal("inspect.process missing observation")
	}
	if err := response.Process.Validate(); err != nil {
		t.Fatalf("invalid process observation: %v", err)
	}
	return *response.Process
}

func killA25Session(t *testing.T, client *ipcadapter.Client, sessionID string) {
	t.Helper()
	callA25V2(t, client, ipcadapter.RequestV2{Action: "kill", RequestID: "a25-kill", SessionID: sessionID, KillID: "a25-kill", Signal: "KILL"})
	for attempt := 0; attempt < 20; attempt++ {
		response := callA25V2(t, client, ipcadapter.RequestV2{Action: "poll", RequestID: "a25-poll", SessionID: sessionID, YieldMS: 50, MaxOutputBytes: 4096})
		if response.Result != nil && response.Result.Operation.State == receipt.OperationTerminal {
			return
		}
	}
	t.Fatal("killed session did not become terminal")
}

func assertA25UnavailableSession(t *testing.T, observation processcore.Observation) {
	t.Helper()
	if observation.Quality != processcore.QualityUnavailable || observation.Root != nil || !a25ContainsDiagnostic(observation.DiagnosticCodes, processcore.DiagnosticObservationIncomplete) {
		t.Fatalf("terminal/restarted session observation=%#v", observation)
	}
}

func startA25Daemon(t *testing.T, stateDir, runtimeDir string) (*ipcadapter.Client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runDaemon(ctx, []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"})
	}()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil && !strings.Contains(err.Error(), "closed") {
					t.Errorf("daemon stop: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("daemon did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock")), stop
}

func callA25V2(t *testing.T, client *ipcadapter.Client, request ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	request.IPVersion = 2
	request.Kind = "request"
	if request.RequestID == "" {
		request.RequestID = "a25-request"
	}
	response, err := client.CallV2(context.Background(), request)
	if err != nil || !response.OK {
		t.Fatalf("%s response=%#v envelope=%+v err=%v", request.Action, response, response.Error, err)
	}
	return response
}

func prependA25FakeExecutable(t *testing.T, name, logPath string) {
	t.Helper()
	fakeBin := filepath.Join(t.TempDir(), "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	writeA25ProbeExecutable(t, fakeBin, name, logPath)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeA25ProbeExecutable(t *testing.T, dir, name, logPath string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' \"${0##*/}\" >> " + shellQuoteA25(logPath) + "\nexit 97\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func shellQuoteA25(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func readA25StateTree(t *testing.T, root string) []byte {
	t.Helper()
	var out strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// The daemon persists JSON through same-directory .shellbeam-* files that
		// are atomically renamed into place. They are transaction mechanics, not
		// durable state. A live scan may observe the directory entry just before
		// rename/remove and then receive ENOENT from Walk's lstat on that path.
		// Ignore both forms so this assertion measures persisted state only.
		if strings.HasPrefix(filepath.Base(path), ".shellbeam-") {
			if err == nil || os.IsNotExist(err) {
				return nil
			}
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(out.String())
}

func TestReadA25StateTreeIgnoresAtomicTransactionTemps(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "durable.json"), []byte("durable-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".shellbeam-transaction"), []byte("transient-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := string(readA25StateTree(t, root)); got != "durable-state" {
		t.Fatalf("state tree included atomic transaction temp: %q", got)
	}
}

func a25SHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func a25ContainsDiagnostic(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func equalA25Ints(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
