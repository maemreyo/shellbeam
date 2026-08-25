//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	supervisoradapter "github.com/maemreyo/shellbeam/internal/adapter/supervisor"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type b1NativeDaemon struct {
	cmd     *exec.Cmd
	client  *ipcadapter.Client
	log     *os.File
	logPath string
	running bool
}

func buildB1NativeBinary(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shellbeam-b1-bin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	binary := filepath.Join(root, "shellbeam")
	cmd := exec.Command("go", "build", "-tags", "shellbeam_native_test", "-o", binary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native binary: %v\n%s", err, out)
	}
	return binary
}

func b1NativeDirs(t *testing.T) (string, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "shellbeam-b1-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return filepath.Join(root, "state"), filepath.Join(root, "run")
}

func startB1NativeDaemon(t *testing.T, binary, stateDir, runtimeDir string) *b1NativeDaemon {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(stateDir), "daemon.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "daemon", "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	d := &b1NativeDaemon{cmd: cmd, log: log, logPath: logPath, running: true}
	d.client = ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock"))
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response, callErr := d.client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "b1-ready", Action: "inspect.server"})
		if callErr == nil && response.OK {
			return d
		}
		time.Sleep(25 * time.Millisecond)
	}
	d.hardKill(t)
	data, _ := os.ReadFile(logPath)
	t.Fatalf("daemon did not become ready: %s", data)
	return nil
}

func (d *b1NativeDaemon) hardKill(t *testing.T) {
	t.Helper()
	if d == nil || !d.running {
		return
	}
	if err := d.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill daemon: %v", err)
	}
	_ = d.cmd.Wait()
	_ = d.log.Close()
	d.running = false
}

func callB1NativeDaemon(t *testing.T, daemon *b1NativeDaemon, req ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	req.IPVersion, req.Kind = 2, "request"
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("b1-%d", time.Now().UnixNano())
	}
	response, err := daemon.client.CallV2(context.Background(), req)
	if err != nil || !response.OK {
		data, _ := os.ReadFile(daemon.logPath)
		t.Fatalf("%s call ok=%v ipc_error=%#v err=%v daemon_log=%s", req.Action, response.OK, response.Error, err, data)
	}
	return response
}

func callB1Native(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	req.IPVersion, req.Kind = 2, "request"
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("b1-%d", time.Now().UnixNano())
	}
	response, err := client.CallV2(context.Background(), req)
	if err != nil {
		t.Fatalf("%s call: %v", req.Action, err)
	}
	if !response.OK {
		t.Fatalf("%s failed: %#v", req.Action, response)
	}
	return response
}

func waitB1NativeSessionSummary(t *testing.T, client *ipcadapter.Client, name string) persistentcore.Summary {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		response := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.sessions", SessionName: name, MaxRecords: 10})
		if response.Sessions != nil && len(response.Sessions.Sessions) == 1 {
			return response.Sessions.Sessions[0]
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %q not discoverable", name)
	return persistentcore.Summary{}
}

func waitB1NativeSessionPID(t *testing.T, client *ipcadapter.Client, sessionID string) int {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		response := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.process", ProcessTarget: &processcore.Target{Kind: processcore.TargetSession, SessionID: sessionID}})
		if response.Process != nil && response.Process.Root != nil && response.Process.Root.PID > 0 {
			return response.Process.Root.PID
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s has no current PID proof", sessionID)
	return 0
}

func waitB1NativeOutputContains(t *testing.T, client *ipcadapter.Client, sessionID, needle string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := callB1Native(t, client, ipcadapter.RequestV2{Action: "poll", SessionID: sessionID, MaxOutputBytes: 8192})
		if response.Result != nil && strings.Contains(response.Result.Output.Preview, needle) {
			return response.Result.Output.Preview
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s output never contained %q", sessionID, needle)
	return ""
}

func waitB1NativeTerminal(t *testing.T, client *ipcadapter.Client, sessionID string) *receipt.Result {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := callB1Native(t, client, ipcadapter.RequestV2{Action: "poll", SessionID: sessionID, MaxOutputBytes: 8192})
		if response.Result != nil && response.Result.Operation.State == receipt.OperationTerminal {
			return response.Result
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s did not become terminal", sessionID)
	return nil
}

func TestB1NativeUnusableSupervisorSocketPathCanonicalizesPrebindingFailure(t *testing.T) {
	binary := buildB1NativeBinary(t)
	root, err := os.MkdirTemp("/tmp", "sb-bnd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	runtimeDir := filepath.Join(root, strings.Repeat("r", 33))
	daemon := startB1NativeDaemon(t, binary, filepath.Join(root, "state"), runtimeDir)
	defer daemon.hardKill(t)

	response, callErr := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "b1-long-socket", Action: "start",
		OperationID: "b1-long-socket", Argv: []string{"/bin/sleep", "30"}, CWD: "/tmp",
		Persistent: true, SessionName: "b1-long-socket", YieldMS: 100, MaxOutputBytes: 4096,
	})
	if callErr != nil || response.OK || response.Error == nil || response.Error.Code != string(failure.SupervisorStateConflict) || response.Error.Details["reason"] != "socket_path" {
		t.Fatalf("start response=%#v err=%v", response, callErr)
	}

	filtered := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "inspect.sessions", SessionName: "b1-long-socket", MaxRecords: 10})
	if filtered.Sessions == nil || len(filtered.Sessions.Sessions) != 1 {
		t.Fatalf("filtered sessions=%#v", filtered.Sessions)
	}
	summary := filtered.Sessions.Sessions[0]
	if summary.State != "failed" || summary.Outcome != "failure" || !summary.Persistent || summary.OwnershipStatus == persistentcore.OwnershipCurrent {
		t.Fatalf("prebinding summary=%#v", summary)
	}
	terminal := waitB1NativeTerminal(t, daemon.client, summary.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "failed" || terminal.Receipt.Outcome != "failure" || terminal.Receipt.Spawn.Attempted || terminal.Receipt.Spawn.Succeeded {
		t.Fatalf("prebinding terminal=%#v", terminal)
	}
	broad := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "inspect.sessions", MaxRecords: 100})
	if broad.Sessions == nil || len(broad.Sessions.Sessions) == 0 {
		t.Fatalf("broad sessions=%#v", broad.Sessions)
	}
	matches, err := filepath.Glob(filepath.Join(runtimeDir, "supervisors", "*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("unexpected supervisor private state=%v err=%v", matches, err)
	}
}

func TestB1NativeDaemonCrashReattachesPersistentAndAbandonsDirect(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)

	persistent := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-persistent", CWD: "/tmp",
		Command:    "trap \"echo TERM_SEEN\" TERM; echo BEFORE; sleep 2; read line; echo GOT:$line; while :; do sleep 1; done",
		Persistent: true, SessionName: "native-dev", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if persistent.Result == nil || persistent.Result.Operation.SessionID == "" || persistent.Result.Operation.State == receipt.OperationTerminal {
		t.Fatalf("persistent start=%#v", persistent)
	}
	sessionID := persistent.Result.Operation.SessionID
	beforePID := waitB1NativeSessionPID(t, first.client, sessionID)

	direct := callB1Native(t, first.client, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-direct", CWD: "/tmp", Command: "sleep 5", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if direct.Result == nil || direct.Result.Operation.SessionID == "" {
		t.Fatalf("direct start=%#v", direct)
	}
	directSessionID := direct.Result.Operation.SessionID

	callB1Native(t, first.client, ipcadapter.RequestV2{Action: "write", SessionID: sessionID, InputOffset: 0, Chars: "hello\n"})
	first.hardKill(t)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	summary := waitB1NativeSessionSummary(t, second.client, "native-dev")
	if summary.SessionID != sessionID || summary.OwnershipStatus != persistentcore.OwnershipReattached {
		t.Fatalf("reattached summary=%#v", summary)
	}
	afterPID := waitB1NativeSessionPID(t, second.client, sessionID)
	if afterPID != beforePID {
		t.Fatalf("reattached pid=%d want %d", afterPID, beforePID)
	}

	callB1Native(t, second.client, ipcadapter.RequestV2{Action: "write", SessionID: sessionID, InputOffset: 0, Chars: "hello\n"})
	workOutput := waitB1NativeOutputContains(t, second.client, sessionID, "GOT:hello")
	if strings.Count(workOutput, "GOT:hello") != 1 {
		t.Fatalf("duplicate input delivery output=%q", workOutput)
	}

	callB1Native(t, second.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-term", Signal: "TERM"})
	waitB1NativeOutputContains(t, second.client, sessionID, "TERM_SEEN")
	callB1Native(t, second.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-term", Signal: "TERM"})
	termOutput := waitB1NativeOutputContains(t, second.client, sessionID, "TERM_SEEN")
	if strings.Count(termOutput, "TERM_SEEN") != 1 {
		t.Fatalf("duplicate TERM signal output=%q", termOutput)
	}

	callB1Native(t, second.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "b1-final-kill", Signal: "KILL"})
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || !terminal.Receipt.Persistent || terminal.Receipt.State != "killed" {
		t.Fatalf("persistent terminal=%#v", terminal)
	}

	directTerminal := waitB1NativeTerminal(t, second.client, directSessionID)
	if directTerminal.Receipt == nil || directTerminal.Receipt.State != "abandoned" || directTerminal.Receipt.Outcome != "ambiguous" {
		t.Fatalf("direct restart terminal=%#v", directTerminal)
	}
}

func TestB1NativeHiddenSupervisorInheritedFDs(t *testing.T) {
	binary := buildB1NativeBinary(t)
	_, runtimeDir := b1NativeDirs(t)
	capability, err := supervisoradapter.NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "01M049DIAGNOSTIC0000000000"
	generationID := "generation-diagnostic"
	layout, err := supervisoradapter.PreparePrivateState(runtimeDir, sessionID, generationID, capability)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := supervisoradapter.Bootstrap{
		SchemaVersion: supervisoradapter.BootstrapSchemaVersion, RuntimeRoot: runtimeDir, SessionID: sessionID, GenerationID: generationID,
		Execution:      supervisoradapter.BootstrapExecution{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 30", CWD: "/tmp"},
		MaxOutputBytes: 4096, MaxQueuedInputBytes: 1024, MaxInputRecords: 16, MaxInputMetadataBytes: 4096, MaxKillRecords: 8, TerminationGraceMS: 100,
	}
	bootstrapRead, bootstrapWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	capabilityRead, capabilityWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "__supervisor")
	cmd.ExtraFiles = []*os.File{bootstrapRead, capabilityRead}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = bootstrapRead.Close()
	_ = capabilityRead.Close()
	if err := supervisoradapter.EncodeBootstrap(bootstrapWrite, bootstrap); err != nil {
		t.Fatal(err)
	}
	_ = bootstrapWrite.Close()
	if err := supervisoradapter.EncodeCapability(capabilityWrite, capability); err != nil {
		t.Fatal(err)
	}
	_ = capabilityWrite.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(layout.SocketPath); err == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	killErr := cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatalf("hidden supervisor did not publish socket kill_err=%v stderr=%q", killErr, stderr.String())
}

func TestB1NativeChildExitWhileDaemonAbsentCanonicalizesTerminalOnce(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-natural-exit", CWD: "/tmp",
		Command: "sleep 1; echo NATURAL_DONE", Persistent: true, SessionName: "native-natural", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	first.hardKill(t)
	time.Sleep(1400 * time.Millisecond)

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "completed" || terminal.Receipt.Outcome != "success" || !terminal.Receipt.Persistent {
		t.Fatalf("natural terminal=%#v", terminal)
	}
	if !strings.Contains(terminal.Output.Preview, "NATURAL_DONE") {
		t.Fatalf("natural output=%q", terminal.Output.Preview)
	}
	summary := waitB1NativeSessionSummary(t, second.client, "native-natural")
	if summary.OwnershipStatus != persistentcore.OwnershipTerminal {
		t.Fatalf("natural summary=%#v", summary)
	}
	if got := countB1NativeEventKind(t, second.client, "b1-native-natural-exit", observation.EventPersistentSessionTerminal); got != 1 {
		t.Fatalf("terminal events=%d", got)
	}
	second.hardKill(t)
	third := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer third.hardKill(t)
	if got := countB1NativeEventKind(t, third.client, "b1-native-natural-exit", observation.EventPersistentSessionTerminal); got != 1 {
		t.Fatalf("terminal events after second restart=%d", got)
	}
}

func TestB1NativeTimeoutFiresWhileDaemonAbsentWithoutDeadlineExtension(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-timeout", CWD: "/tmp", Command: "sleep 30",
		Persistent: true, SessionName: "native-timeout", TimeoutMS: 700, YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	first.hardKill(t)
	time.Sleep(1100 * time.Millisecond)

	restartedAt := time.Now()
	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "timed_out" || terminal.Receipt.Outcome != "timeout" || !terminal.Receipt.Persistent {
		t.Fatalf("timeout terminal=%#v", terminal)
	}
	if time.Since(restartedAt) > 2*time.Second {
		t.Fatalf("timeout deadline appears extended after restart: %v", time.Since(restartedAt))
	}
}

func TestB1NativeMissingCapabilityClassifiesLostWithoutSignalingChild(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-missing-cap", CWD: "/tmp", Command: "exec sleep 30",
		Persistent: true, SessionName: "native-missing-cap", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	childPID := waitB1NativeSessionPID(t, first.client, sessionID)
	first.hardKill(t)

	capabilityPath := filepath.Join(runtimeDir, "supervisors", sessionID, "capability.bin")
	if err := os.Remove(capabilityPath); err != nil {
		t.Fatal(err)
	}
	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	summary := waitB1NativeSessionSummary(t, second.client, "native-missing-cap")
	if summary.OwnershipStatus != persistentcore.OwnershipLost {
		t.Fatalf("lost summary=%#v", summary)
	}
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "abandoned" || terminal.Receipt.Outcome != "ambiguous" {
		t.Fatalf("lost terminal=%#v", terminal)
	}
	if !b1ProcessAlive(childPID) {
		t.Fatalf("child pid %d was signaled/reaped after ownership loss", childPID)
	}
	process, _ := os.FindProcess(childPID)
	_ = process.Signal(syscall.SIGKILL)
}

func b1ProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func b1SupervisorChildren(t *testing.T, daemonPID int, binary string) []int {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	var result []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil || ppid != daemonPID {
			continue
		}
		command := strings.Join(fields[2:], " ")
		if strings.Contains(command, binary) && strings.Contains(command, "__supervisor") {
			result = append(result, pid)
		}
	}
	return result
}

func killB1Process(t *testing.T, pid int) {
	t.Helper()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !b1ProcessAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestB1NativeMissingSupervisorClassifiesLostWithoutReclaimingChildPID(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-missing-supervisor", CWD: "/tmp", Command: "exec sleep 30",
		Persistent: true, SessionName: "native-missing-supervisor", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	childPID := waitB1NativeSessionPID(t, first.client, sessionID)
	supervisors := b1SupervisorChildren(t, first.cmd.Process.Pid, binary)
	if len(supervisors) != 1 {
		t.Fatalf("supervisor children=%v", supervisors)
	}
	first.hardKill(t)
	killB1Process(t, supervisors[0])
	if !b1ProcessAlive(childPID) {
		t.Fatalf("child pid %d died with supervisor", childPID)
	}

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	summary := waitB1NativeSessionSummary(t, second.client, "native-missing-supervisor")
	if summary.OwnershipStatus != persistentcore.OwnershipLost {
		t.Fatalf("lost summary=%#v", summary)
	}
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "abandoned" || terminal.Receipt.Outcome != "ambiguous" {
		t.Fatalf("lost terminal=%#v", terminal)
	}
	if !b1ProcessAlive(childPID) {
		t.Fatalf("child pid %d was reclaimed/signaled by restarted daemon", childPID)
	}
	process, _ := os.FindProcess(childPID)
	_ = process.Signal(syscall.SIGKILL)
}

func TestB1NativeGenerationAndProtocolMismatchClassifyLostWithoutSignalingChild(t *testing.T) {
	binary := buildB1NativeBinary(t)
	for _, mode := range []string{"generation", "protocol"} {
		t.Run(mode, func(t *testing.T) { runB1NativeMetadataLossCase(t, binary, mode) })
	}
}

func runB1NativeMetadataLossCase(t *testing.T, binary, mode string) {
	t.Helper()
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	name := "native-meta-" + mode
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-meta-" + mode, CWD: "/tmp", Command: "exec sleep 30",
		Persistent: true, SessionName: name, YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	childPID := waitB1NativeSessionPID(t, first.client, sessionID)
	first.hardKill(t)

	metadataPath := filepath.Join(runtimeDir, "supervisors", sessionID, "metadata.json")
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata supervisoradapter.Metadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if mode == "generation" {
		metadata.GenerationID = "generation-tampered"
	} else {
		metadata.ProtocolVersion++
	}
	raw, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	summary := waitB1NativeSessionSummary(t, second.client, name)
	if summary.OwnershipStatus != persistentcore.OwnershipLost {
		t.Fatalf("lost summary=%#v", summary)
	}
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "abandoned" || terminal.Receipt.Outcome != "ambiguous" {
		t.Fatalf("lost terminal=%#v", terminal)
	}
	if !b1ProcessAlive(childPID) {
		t.Fatalf("child pid %d was signaled after %s mismatch", childPID, mode)
	}
	process, _ := os.FindProcess(childPID)
	_ = process.Signal(syscall.SIGKILL)
}

func TestB1NativeCorruptTerminalRecordClassifiesLostWithoutRelaunch(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-corrupt-terminal", CWD: "/tmp", Command: "sleep 1; echo TERMINAL_WRITTEN",
		Persistent: true, SessionName: "native-corrupt-terminal", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	first.hardKill(t)
	time.Sleep(1400 * time.Millisecond)
	terminalPath := filepath.Join(runtimeDir, "supervisors", sessionID, "terminal.json")
	if _, err := os.Stat(terminalPath); err != nil {
		t.Fatalf("terminal record missing before corruption: %v", err)
	}
	if err := os.WriteFile(terminalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	second := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer second.hardKill(t)
	summary := waitB1NativeSessionSummary(t, second.client, "native-corrupt-terminal")
	if summary.OwnershipStatus != persistentcore.OwnershipLost {
		t.Fatalf("lost summary=%#v", summary)
	}
	terminal := waitB1NativeTerminal(t, second.client, sessionID)
	if terminal.Receipt == nil || terminal.Receipt.State != "abandoned" || terminal.Receipt.Outcome != "ambiguous" {
		t.Fatalf("corrupt terminal result=%#v", terminal)
	}
	if supervisors := b1SupervisorChildren(t, second.cmd.Process.Pid, binary); len(supervisors) != 0 {
		t.Fatalf("startup relaunched supervisor after corrupt terminal: %v", supervisors)
	}
}

func TestB1NativeCapabilitySecretStaysOutOfPublicAndDiagnosticSurfaces(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	daemon := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer daemon.hardKill(t)
	started := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{
		Action: "start", OperationID: "b1-native-privacy", CWD: "/tmp", Command: "exec sleep 30",
		Persistent: true, SessionName: "native-privacy", YieldMS: 20, MaxOutputBytes: 4096,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("start missing session")
	}
	sessionID := started.Result.Operation.SessionID
	capabilityPath := filepath.Join(runtimeDir, "supervisors", sessionID, "capability.bin")
	secret, err := os.ReadFile(capabilityPath)
	if err != nil || len(secret) == 0 {
		t.Fatalf("capability unavailable for privacy assertion: %v", err)
	}
	variants := b1SecretVariants(secret)

	public := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "inspect.sessions", SessionName: "native-privacy", MaxRecords: 10})
	publicJSON, err := json.Marshal(struct {
		Start   ipcadapter.ResponseV2 `json:"start"`
		Inspect ipcadapter.ResponseV2 `json:"inspect"`
	}{started, public})
	if err != nil {
		t.Fatal(err)
	}
	assertB1NoSecretVariants(t, "public IPC JSON", publicJSON, variants)
	logBytes, _ := os.ReadFile(daemon.logPath)
	assertB1NoSecretVariants(t, "daemon log", logBytes, variants)
	serviceSource, _ := os.ReadFile("command_service.go")
	assertB1NoSecretVariants(t, "launchd/systemd service definition templates", serviceSource, variants)

	for _, pid := range append([]int{daemon.cmd.Process.Pid}, b1SupervisorChildren(t, daemon.cmd.Process.Pid, binary)...) {
		out, _ := exec.Command("ps", "eww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		assertB1NoSecretVariants(t, "process argv/env", out, variants)
	}
	assertB1TreeNoSecret(t, stateDir, "canonical state", variants, "")
	assertB1TreeNoSecret(t, filepath.Join(runtimeDir, "supervisors", sessionID), "supervisor metadata", variants, capabilityPath)

	callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "kill", SessionID: sessionID, KillID: "privacy-cleanup", Signal: "KILL"})
	_ = waitB1NativeTerminal(t, daemon.client, sessionID)
}

func b1SecretVariants(secret []byte) [][]byte {
	return [][]byte{append([]byte(nil), secret...), []byte(hex.EncodeToString(secret)), []byte(base64.StdEncoding.EncodeToString(secret)), []byte(base64.RawURLEncoding.EncodeToString(secret))}
}

func assertB1NoSecretVariants(t *testing.T, surface string, data []byte, variants [][]byte) {
	t.Helper()
	for _, variant := range variants {
		if len(variant) > 0 && bytes.Contains(data, variant) {
			t.Fatalf("capability secret leaked in %s", surface)
		}
	}
}

func assertB1TreeNoSecret(t *testing.T, root, surface string, variants [][]byte, skip string) {
	t.Helper()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() || path == skip {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		assertB1NoSecretVariants(t, surface+":"+filepath.Base(path), data, variants)
		return nil
	})
}

func countB1NativeEventKind(t *testing.T, client *ipcadapter.Client, operationID string, kind observation.EventKind) int {
	t.Helper()
	response := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: operationID}, MaxEvents: 64})
	if response.Events == nil {
		t.Fatalf("inspect.events missing response")
	}
	count := 0
	for _, event := range response.Events.Events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func TestB1NativeOrdinaryStartCreatesNoPersistentRuntimeState(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	daemon := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer daemon.hardKill(t)
	started := callB1Native(t, daemon.client, ipcadapter.RequestV2{Action: "start", OperationID: "b1-native-ordinary-zero", CWD: "/tmp", Command: "sleep 1", YieldMS: 20, MaxOutputBytes: 4096})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		t.Fatalf("ordinary start missing session")
	}
	if got := b1SupervisorChildren(t, daemon.cmd.Process.Pid, binary); len(got) != 0 {
		t.Fatalf("ordinary start spawned supervisors: %v", got)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "supervisors")); !os.IsNotExist(err) {
		t.Fatalf("ordinary start touched private supervisor root: %v", err)
	}
	for _, dir := range []string{"bindings", "names", "active"} {
		entries, err := os.ReadDir(filepath.Join(stateDir, "persistent-sessions", dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("ordinary start wrote persistent %s records: %d", dir, len(entries))
		}
	}
}
