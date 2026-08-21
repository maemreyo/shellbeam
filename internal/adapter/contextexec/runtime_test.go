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
	terminal, err := runtime.Execute(context.Background(), request, func(frame OutputFrame) error { frames = append(frames, frame); return nil })
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
	if terminal.Result.Output.Attribution != core.OutputAttributionHelperOwnedChildPipes || terminal.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 || !terminal.Result.Exit.Reaped {
		t.Fatalf("result=%#v", terminal.Result)
	}
}

func TestRuntimeMarksTruncatedOutputIncompleteWithoutDroppingChildOwnedAuthority(t *testing.T) {
	cwd := t.TempDir()
	request := runtimeRequestFrame(t, cwd, []string{"/bin/sh", "-c", `printf '1234567890'; printf 'abcdefghij' >&2`}, 8, 2000)
	runtime := Runtime{Launcher: testStrongLauncher{}, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}
	terminal, err := runtime.Execute(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !terminal.Result.Output.Truncated || terminal.Result.Output.OutputComplete || terminal.Result.EvidenceQuality != core.EvidenceQualityIncomplete || terminal.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 {
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
	terminal, err := runtime.Execute(context.Background(), request, nil)
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
	if _, err := runtime.Execute(context.Background(), request, nil); err == nil {
		t.Fatal("unqualified executor accepted")
	}
	if launcher.calls != 0 {
		t.Fatalf("spawn calls=%d", launcher.calls)
	}
}

type countingUnqualifiedLauncher struct{ calls int }

func (*countingUnqualifiedLauncher) Qualified() bool { return false }
func (l *countingUnqualifiedLauncher) Launch(ChildSpec) (*ChildProcess, error) {
	l.calls++
	return nil, errors.New("must not launch")
}

type testStrongLauncher struct{}

func (testStrongLauncher) Qualified() bool { return true }
func (testStrongLauncher) Launch(spec ChildSpec) (*ChildProcess, error) {
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
	cmd := exec.Command(resolved, spec.Argv[1:]...)
	cmd.Dir = spec.CWD
	cmd.Env = append([]string(nil), spec.Env...)
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
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
	return &ChildProcess{ResolvedExecutable: resolved, Stdout: stdout, Stderr: stderr, Wait: wait, KillGroup: kill}, nil
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
