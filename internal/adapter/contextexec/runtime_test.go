package contextexec

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

func TestRuntimeOwnsRealChildContextPipesAndDoesNotAttributePaneNoise(t *testing.T) {
	cwd := t.TempDir()
	secret := "h5-runtime-user-canary"
	request := runtimeRequestFrame(t, cwd, []string{"/bin/sh", "-c", `printf 'OUT:%s:%s' "$H5_SECRET" "$PWD"; printf 'ERR:%s' "${SHELLBEAM_CONTEXT_EXEC_CLAIM-unset}" >&2`}, 1<<20, 2000)
	runtime := Runtime{Launcher: testStrongLauncher{}, Environ: func() []string {
		return []string{"PATH=/usr/bin:/bin", "H5_SECRET=" + secret, "SHELLBEAM_CONTEXT_EXEC_CLAIM=must-not-reach-child"}
	}, Getwd: func() (string, error) { return cwd, nil }}
	paneNoise := "PANE_NOISE_SHOULD_NEVER_APPEAR"
	_ = paneNoise
	var frames []OutputFrame
	peer := &testExecutionProtocol{onOutput: func(frame OutputFrame) error { frames = append(frames, frame); return nil }}
	terminal, err := runtime.Execute(context.Background(), request, peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Validate(); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	stdout, stderr := joinFrames(frames)
	physicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "OUT:"+secret+":"+physicalCWD) && !strings.Contains(stdout, "OUT:"+secret+":"+cwd) {
		t.Fatalf("stdout=%q cwd=%q physical=%q", stdout, cwd, physicalCWD)
	}
	if stderr != "ERR:unset" {
		t.Fatalf("stderr=%q", stderr)
	}
	if strings.Contains(stdout+stderr, paneNoise) {
		t.Fatal("pane noise entered authoritative output")
	}
	if terminal.Result.Output.Attribution != core.OutputAttributionHelperOwnedChildPipes || terminal.Result.Lifecycle != core.LifecycleChildTerminal || terminal.Result.EvidenceAuthority != "" || !terminal.Result.Exit.Reaped {
		t.Fatalf("result=%#v", terminal.Result)
	}
}

func TestRuntimeMarksTruncatedOutputIncompleteWithoutDroppingChildOwnedAuthority(t *testing.T) {
	cwd := t.TempDir()
	request := runtimeRequestFrame(t, cwd, []string{"/bin/sh", "-c", `printf '1234567890'; printf 'abcdefghij' >&2`}, 8, 2000)
	runtime := Runtime{Launcher: testStrongLauncher{}, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}
	terminal, err := runtime.Execute(context.Background(), request, &testExecutionProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	if !terminal.Result.Output.Truncated || terminal.Result.Output.OutputComplete || terminal.Result.EvidenceQuality != core.EvidenceQualityIncomplete || terminal.Result.Lifecycle != core.LifecycleChildTerminal || terminal.Result.EvidenceAuthority != "" {
		t.Fatalf("result=%#v", terminal.Result)
	}
	if err := terminal.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeTimeoutKillsOwnedProcessGroupAndReaps(t *testing.T) {
	cwd := t.TempDir()
	request := runtimeRequestFrame(t, cwd, []string{"/bin/sh", "-c", `sleep 5`}, 1024, 80)
	runtime := Runtime{Launcher: testStrongLauncher{}, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}
	started := time.Now()
	terminal, err := runtime.Execute(context.Background(), request, &testExecutionProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("timeout did not bound child")
	}
	if !terminal.Result.TimedOut || !terminal.Result.Signal.Attempted || !terminal.Result.Signal.Succeeded || !terminal.Result.Exit.Reaped {
		t.Fatalf("result=%#v", terminal.Result)
	}
}

func TestRuntimeFailsClosedBeforeSpawnWhenStrongExecutorUnavailable(t *testing.T) {
	cwd := t.TempDir()
	launcher := &countingUnqualifiedLauncher{}
	request := runtimeRequestFrame(t, cwd, []string{"/bin/echo", "no"}, 1024, 1000)
	runtime := Runtime{Launcher: launcher, Environ: os.Environ, Getwd: func() (string, error) { return cwd, nil }}
	if _, err := runtime.Execute(context.Background(), request, &testExecutionProtocol{}); err == nil {
		t.Fatal("unqualified executor accepted")
	}
	if launcher.calls != 0 {
		t.Fatalf("spawn calls=%d", launcher.calls)
	}
}

type testExecutionProtocol struct {
	onOutput func(OutputFrame) error
}

func (*testExecutionProtocol) AuthorizePrepared(_ context.Context, frame PreparedFrame) (ExecuteFrame, error) {
	if err := frame.Validate(); err != nil {
		return ExecuteFrame{}, err
	}
	if frame.FailureCode != "" {
		return ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: false}, nil
	}
	return ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}, nil
}
func (*testExecutionProtocol) SendSpawn(frame SpawnFrame) error { return frame.Validate() }
func (p *testExecutionProtocol) SendOutput(frame OutputFrame) error {
	if p.onOutput != nil {
		return p.onOutput(frame)
	}
	return nil
}

type countingUnqualifiedLauncher struct{ calls int }

func (*countingUnqualifiedLauncher) Qualified() bool { return false }
func (l *countingUnqualifiedLauncher) Prepare(ChildSpec) (PreparedExecution, error) {
	l.calls++
	return nil, errors.New("must not prepare")
}

type testStrongLauncher struct{}

func (testStrongLauncher) Qualified() bool { return true }
func (testStrongLauncher) Prepare(spec ChildSpec) (PreparedExecution, error) {
	resolved, err := exec.LookPath(spec.Argv[0])
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(resolved) {
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, err
		}
	}
	return &testStrongPrepared{spec: spec, resolved: resolved}, nil
}

type testStrongPrepared struct {
	spec     ChildSpec
	resolved string
}

func (p *testStrongPrepared) ResolvedExecutable() string { return p.resolved }
func (*testStrongPrepared) Close() error                 { return nil }
func (p *testStrongPrepared) Start() (*ChildProcess, error) {
	cmd := exec.Command(p.resolved, p.spec.Argv[1:]...)
	cmd.Dir = p.spec.CWD
	cmd.Env = append([]string(nil), p.spec.Env...)
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pipes, err := attachChildOutputPipes(cmd)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	if err := pipes.closeParentWriters(); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		_ = pipes.closeAll()
		return nil, err
	}
	var once sync.Once
	var exit ChildExit
	var waitErr error
	wait := func() (ChildExit, error) {
		once.Do(func() {
			err := cmd.Wait()
			if err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					waitErr = err
					return
				}
			}
			exit.Reaped = true
			if code := cmd.ProcessState.ExitCode(); code >= 0 {
				exit.Code = &code
			}
			if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				exit.Signal = ws.Signal().String()
			}
		})
		return exit, waitErr
	}
	kill := func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return &ChildProcess{ResolvedExecutable: p.resolved, Stdout: pipes.stdoutR, Stderr: pipes.stderrR, Wait: wait, KillGroup: kill}, nil
}

func runtimeRequestFrame(t *testing.T, cwd string, argv []string, maxOutput, timeout int64) RequestFrame {
	t.Helper()
	req := core.Request{ContextExecID: "ctxexec_runtime_01", SessionID: "session_runtime_01", AuthorityEpoch: 4, Argv: argv, TimeoutMS: timeout, MaxOutputBytes: maxOutput}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return RequestFrame{ProtocolVersion: ProtocolVersion, Kind: KindRequest, Request: req, Context: core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: "fish:runtime_01", BoundaryQuality: "shell_prompt", CWDObserved: cwd, PrivacyState: "standard"}, Helper: core.HelperBinding{OpaqueLaunchID: "launch_runtime_01", Generation: "generation_runtime_01", RequestFingerprint: fp, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}}
}
func joinFrames(frames []OutputFrame) (string, string) {
	var out, err strings.Builder
	for _, f := range frames {
		if f.Stream == StreamStdout {
			out.Write(f.Data)
		} else if f.Stream == StreamStderr {
			err.Write(f.Data)
		}
	}
	return out.String(), err.String()
}

var _ io.Reader

func TestRuntimeReportsChildTerminalWithoutDaemonEvidenceAuthority(t *testing.T) {
	cwd := t.TempDir()
	request := runtimeRequestFrame(t, cwd, []string{"/bin/echo", "ok"}, 1024, 1000)
	runtime := Runtime{Launcher: testStrongLauncher{}, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}
	terminal, err := runtime.Execute(context.Background(), request, &testExecutionProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Result.Lifecycle != core.LifecycleChildTerminal {
		t.Fatalf("helper lifecycle=%q want child_terminal", terminal.Result.Lifecycle)
	}
	if terminal.Result.EvidenceAuthority != "" {
		t.Fatalf("helper claimed daemon evidence authority %q", terminal.Result.EvidenceAuthority)
	}
	if err := terminal.Validate(); err != nil {
		t.Fatalf("child terminal invalid: %v", err)
	}
}
