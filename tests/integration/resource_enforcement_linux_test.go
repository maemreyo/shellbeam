//go:build linux

package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	corefailure "github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const resourceNativeRootEnv = "SHELLBEAM_RESOURCE_CGROUP_ROOT"

type nativeResourceStatus interface {
	ResourceLimitBreach() operation.ResourceLimitKind
	ResourceCleanupIncomplete() string
}

type nativeResourceRun struct {
	Exit    receipt.ExitEvidence
	Output  string
	Breach  operation.ResourceLimitKind
	Cleanup string
}

func TestResourceEnforcementNativeHelper(t *testing.T) {
	mode := helperMode(os.Args)
	if mode == "" {
		t.Skip("helper subprocess only")
	}
	switch mode {
	case "memory":
		// Touch every page so the cgroup charge is resident rather than merely a
		// virtual reservation. The native test gives this process a much smaller
		// memory.max and expects the kernel to terminate the owned cgroup.
		b := make([]byte, 256<<20)
		for i := 0; i < len(b); i += 4096 {
			b[i] = byte(i)
		}
		runtime.KeepAlive(b)
		time.Sleep(30 * time.Second)
	case "hold":
		time.Sleep(30 * time.Second)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func TestResourceEnforcementNativeDirectBirthShellArgvPTY(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, _ := qualifiedNativeOwner(t)
	limits := &operation.ResourceLimits{Processes: 16}
	cases := []struct {
		name string
		spec operation.ExecutionSpec
	}{
		{name: "shell", spec: operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Command: "cat /proc/self/cgroup", CWD: "/tmp", ResourceLimits: limits}},
		{name: "argv", spec: operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/cat", "/proc/self/cgroup"}, CWD: "/tmp", ResourceLimits: limits}},
		{name: "pty", spec: operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Command: "cat /proc/self/cgroup", CWD: "/tmp", TTY: true, ResourceLimits: limits}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runNativeResourceOwner(t, owner, tc.spec, 10*time.Second)
			if !result.Exit.Reaped || result.Breach != "" || result.Cleanup != "" {
				t.Fatalf("run=%#v", result)
			}
			if !strings.Contains(result.Output, "/job-") {
				t.Fatalf("first child observation does not show operation cgroup: %q", result.Output)
			}
			assertNoNativeJobChildren(t, root)
		})
	}
}

func TestResourceEnforcementNativeDescendantMemoryOOMAndTypedReceipt(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, support := qualifiedNativeOwner(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := shellQuote(exe) + " -test.run '^TestResourceEnforcementNativeHelper$' -- memory"

	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := storeadapter.Open(stateRoot, storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 26, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	catalog := capability.Baseline(capability.Limits{}).WithResourceEnforcement(*support)
	svc := daemonapp.NewService(store, owner, daemonapp.Options{
		Incarnation: "resource-native-memory", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		DefaultTimeoutMS: 30000, MaxTimeoutMS: 60000, TerminationGrace: 100 * time.Millisecond, Capabilities: catalog,
	})
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	limits := &operation.ResourceLimits{MemoryBytes: 96 << 20, Processes: 16}
	view, err := svc.Start(context.Background(), daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "resource-native-memory", Command: command, CWD: "/tmp", ResourceLimits: limits,
		YieldMS: 0, MaxOutputBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	view = waitNativeTerminal(t, svc, view.SessionID, 20*time.Second)
	if view.State != session.Failed || view.Outcome != session.Failure || view.Receipt == nil {
		t.Fatalf("terminal=%#v", view)
	}
	if view.Receipt.FailureReason != "resource_limit_memory" || view.Failure == nil || view.Failure.Class != receipt.ClassResource || view.Failure.Code != "resource_limit" || view.Failure.Details["resource_limit_kind"] != "memory" {
		t.Fatalf("typed resource failure receipt=%#v failure=%#v", view.Receipt, view.Failure)
	}
	if !view.Receipt.Exit.Reaped || view.Receipt.Exit.Code != nil || view.Receipt.Exit.Signal != "killed" {
		t.Fatalf("expected literal SIGKILL evidence from memory.oom.group, got %#v", view.Receipt.Exit)
	}
	if view.Receipt.ResourceCleanup != nil {
		t.Fatalf("normal memory cleanup reported defect: %#v", view.Receipt.ResourceCleanup)
	}
	assertNoNativeJobChildren(t, root)
}

func TestResourceEnforcementNativeProcessForkStormIsTerminal(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, _ := qualifiedNativeOwner(t)
	result := runNativeResourceOwner(t, owner, operation.ExecutionSpec{
		Mode: operation.ExecutionModeShell, Shell: "/bin/sh",
		Command: "while :; do /bin/sleep 60 & done", CWD: "/tmp",
		ResourceLimits: &operation.ResourceLimits{Processes: 8},
	}, 10*time.Second)
	if !result.Exit.Reaped {
		t.Fatalf("fork storm was not reaped: %#v", result)
	}
	if result.Breach != operation.ResourceLimitProcesses || result.Cleanup != "" {
		t.Fatalf("fork storm result=%#v", result)
	}
	assertNoNativeJobChildren(t, root)
}

func TestResourceEnforcementNativeNoLimitCreatesNoJobCgroup(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, _ := qualifiedNativeOwner(t)
	assertNoNativeJobChildren(t, root)
	sink := &capture{}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/sleep", "0.2"}, CWD: "/tmp",
	}, sink)
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("ordinary start spawn=%#v err=%v", spawn, err)
	}
	// Inspect while the child is alive: a provider path that merely creates and
	// quickly removes a job would still be caught here.
	assertNoNativeJobChildren(t, root)
	exit := waitNativeHandle(t, h, 5*time.Second)
	if exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("ordinary exit=%#v", exit)
	}
	_ = h.Close()
	assertNoNativeJobChildren(t, root)
}

func TestResourceEnforcementNativeStaleLiveJobRecovery(t *testing.T) {
	root := requireNativeResourceRoot(t)
	assertNoNativeJobChildren(t, root)
	stale := filepath.Join(root, "job-stale-native")
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if _, err := os.Stat(stale); err == nil {
			_ = writeNativeControl(filepath.Join(stale, "cgroup.kill"), "1")
			_ = os.Remove(stale)
		}
	})
	if err := writeNativeControl(filepath.Join(stale, "cgroup.procs"), strconv.Itoa(cmd.Process.Pid)); err != nil {
		t.Fatalf("place stale child: %v", err)
	}
	if !nativeCgroupPopulated(t, stale) {
		t.Fatal("stale recovery fixture was not populated")
	}
	if _, _, err := processadapter.NewOwnerFromEnvironment(); err != nil {
		t.Fatalf("qualification failed to reconcile stale owned job: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		waited = true
	case <-time.After(5 * time.Second):
		t.Fatal("stale descendant survived provider recovery")
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale cgroup survived recovery: %v", err)
	}
	assertNoNativeJobChildren(t, root)
}

func TestResourceEnforcementNativeRepeatedBoundedJobsHaveZeroCreep(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, _ := qualifiedNativeOwner(t)
	for i := 0; i < 100; i++ {
		result := runNativeResourceOwner(t, owner, operation.ExecutionSpec{
			Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/true"}, CWD: "/tmp",
			ResourceLimits: &operation.ResourceLimits{MemoryBytes: 128 << 20, Processes: 8},
		}, 5*time.Second)
		if result.Exit.Code == nil || *result.Exit.Code != 0 || result.Breach != "" || result.Cleanup != "" {
			t.Fatalf("iteration %d result=%#v", i, result)
		}
		assertNoNativeJobChildren(t, root)
	}
	manager := filepath.Join(root, "manager")
	if !nativeCgroupPopulated(t, manager) {
		t.Fatal("reserved manager cgroup unexpectedly became empty during native lane")
	}
}

func TestResourceEnforcementNativeCPUAndPersistentRequestsFailBeforeSpawn(t *testing.T) {
	root := requireNativeResourceRoot(t)
	owner, support := qualifiedNativeOwner(t)
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 26, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	svc := daemonapp.NewService(store, owner, daemonapp.Options{
		Incarnation: "resource-native-refusal", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		Capabilities: capability.Baseline(capability.Limits{}).WithResourceEnforcement(*support),
	})
	marker := filepath.Join(t.TempDir(), "must-not-spawn")
	cases := []struct {
		name string
		req  daemonapp.StartRequest
	}{
		{name: "cpu", req: daemonapp.StartRequest{ProtocolVersion: 2, OperationID: "resource-native-cpu", Command: "touch " + shellQuote(marker), CWD: "/tmp", ResourceLimits: &operation.ResourceLimits{CPUTimeMS: 1000}}},
		{name: "persistent", req: daemonapp.StartRequest{ProtocolVersion: 2, OperationID: "resource-native-persistent", Command: "touch " + shellQuote(marker), CWD: "/tmp", Persistent: true, SessionName: "resource-native", ResourceLimits: &operation.ResourceLimits{Processes: 4}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Start(context.Background(), tc.req)
			var typed *corefailure.Failure
			if !errors.As(err, &typed) || typed.Code != corefailure.ResourceLimitUnsupported {
				t.Fatalf("error=%v typed=%#v", err, typed)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsupported request spawned child: %v", statErr)
			}
			assertNoNativeJobChildren(t, root)
		})
	}
}

func requireNativeResourceRoot(t *testing.T) string {
	t.Helper()
	root := strings.TrimSpace(os.Getenv(resourceNativeRootEnv))
	if root == "" {
		t.Skip("NOT_RUN: native resource cgroup root not provisioned")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		t.Fatalf("invalid native cgroup root configuration")
	}
	return root
}

func qualifiedNativeOwner(t *testing.T) (processadapter.Owner, *capability.ResourceEnforcementSupport) {
	t.Helper()
	owner, support, err := processadapter.NewOwnerFromEnvironment()
	if err != nil {
		t.Fatalf("resource provider qualification: %v", err)
	}
	if support == nil || !support.ValidV1() || support.Provider != "linux_cgroup_v2" {
		t.Fatalf("qualified support=%#v", support)
	}
	return owner, support
}

func runNativeResourceOwner(t *testing.T, owner processadapter.Owner, spec operation.ExecutionSpec, timeout time.Duration) nativeResourceRun {
	t.Helper()
	sink := &capture{}
	h, spawn, err := owner.Start(context.Background(), spec, sink)
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("owner start spawn=%#v err=%v", spawn, err)
	}
	exit := waitNativeHandle(t, h, timeout)
	status, ok := h.(nativeResourceStatus)
	if !ok {
		t.Fatalf("resource-limited handle lost terminal status interface: %T", h)
	}
	result := nativeResourceRun{Exit: exit, Output: nativeCaptureString(sink), Breach: status.ResourceLimitBreach(), Cleanup: status.ResourceCleanupIncomplete()}
	_ = h.Close()
	return result
}

func waitNativeHandle(t *testing.T, h daemonapp.ProcessHandle, timeout time.Duration) receipt.ExitEvidence {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	exit := h.Wait(ctx)
	if exit.Reaped {
		return exit
	}
	_ = h.Signal("KILL")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	exit = h.Wait(ctx2)
	if !exit.Reaped {
		t.Fatal("native child did not reap within bounded timeout")
	}
	return exit
}

func waitNativeTerminal(t *testing.T, svc *daemonapp.Service, sessionID string, timeout time.Duration) daemonapp.View {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := svc.Poll(context.Background(), daemonapp.PollRequest{SessionID: sessionID, Cursor: 0, YieldMS: 20, MaxOutputBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		if view.State.Terminal() {
			return view
		}
	}
	t.Fatalf("session %s did not become terminal", sessionID)
	return daemonapp.View{}
}

func nativeCaptureString(c *capture) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

func nativeJobChildren(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var jobs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "job-") {
			jobs = append(jobs, entry.Name())
		}
	}
	return jobs
}

func assertNoNativeJobChildren(t *testing.T, root string) {
	t.Helper()
	if jobs := nativeJobChildren(t, root); len(jobs) != 0 {
		t.Fatalf("owned cgroup creep: %v", jobs)
	}
}

func nativeCgroupPopulated(t *testing.T, root string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "cgroup.events"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			return fields[1] == "1"
		}
	}
	t.Fatalf("cgroup.events missing populated: %q", data)
	return false
}

func writeNativeControl(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func helperMode(args []string) string {
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
