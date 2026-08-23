//go:build linux

package contextexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type linuxPlatformLauncher struct {
	helperExecutable  string
	nativeH0Qualified bool
}

func NewPlatformLauncher(helperExecutable ...string) ChildLauncher {
	launcher := linuxPlatformLauncher{}
	if len(helperExecutable) > 0 {
		launcher.helperExecutable = helperExecutable[0]
	}
	return launcher
}
func (l linuxPlatformLauncher) Qualified() bool {
	if !l.nativeH0Qualified {
		return false
	}
	info, err := os.Stat("/proc/self/fd")
	return err == nil && info.IsDir()
}
func (l linuxPlatformLauncher) Prepare(spec ChildSpec) (PreparedExecution, error) {
	if !l.Qualified() {
		return nil, fmt.Errorf("descriptor-bound context execution unavailable on unqualified linux")
	}
	if spec.HelperExecutable == "" {
		spec.HelperExecutable = l.helperExecutable
	}
	if spec.HelperExecutable == "" || !filepath.IsAbs(spec.HelperExecutable) {
		return nil, fmt.Errorf("context helper executable identity unavailable")
	}
	targetPath, err := resolveChildExecutable(spec)
	if err != nil {
		return nil, err
	}
	target, err := os.Open(targetPath)
	if err != nil {
		return nil, err
	}
	targetInfo, err := target.Stat()
	if err != nil || !targetInfo.Mode().IsRegular() || targetInfo.Mode().Perm()&0111 == 0 {
		_ = target.Close()
		return nil, fmt.Errorf("invalid opened context executable")
	}
	observed, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(target.Fd())))
	if err != nil || !filepath.IsAbs(observed) {
		_ = target.Close()
		return nil, fmt.Errorf("context executable object identity unavailable")
	}
	helper, err := os.Open(spec.HelperExecutable)
	if err != nil {
		_ = target.Close()
		return nil, err
	}
	helperInfo, err := helper.Stat()
	if err != nil || !helperInfo.Mode().IsRegular() || helperInfo.Mode().Perm()&0111 == 0 {
		_ = helper.Close()
		_ = target.Close()
		return nil, fmt.Errorf("invalid context helper executable")
	}
	return &linuxPreparedExecution{spec: spec, target: target, helper: helper, resolved: observed}, nil
}

type linuxPreparedExecution struct {
	spec     ChildSpec
	target   *os.File
	helper   *os.File
	resolved string
	mu       sync.Mutex
	started  bool
	closed   bool
}

func (p *linuxPreparedExecution) ResolvedExecutable() string {
	if p == nil {
		return ""
	}
	return p.resolved
}

func (p *linuxPreparedExecution) Start() (*ChildProcess, error) {
	if p == nil {
		return nil, fmt.Errorf("prepared context executable unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.started || p.target == nil || p.helper == nil {
		return nil, fmt.Errorf("prepared context executable is not startable")
	}
	p.started = true
	args := append([]string{"__context_exec_fdexec"}, p.spec.Argv...)
	cmd := exec.Command("/proc/self/fd/3", args...)
	cmd.Dir = p.spec.CWD
	cmd.Env = append([]string(nil), p.spec.Env...)
	cmd.Stdin = nil
	cmd.ExtraFiles = []*os.File{p.helper, p.target}
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
		return nil, fmt.Errorf("close parent context child output writers: %w", err)
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

func (p *linuxPreparedExecution) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var err error
	if p.helper != nil {
		err = errors.Join(err, p.helper.Close())
		p.helper = nil
	}
	if p.target != nil {
		err = errors.Join(err, p.target.Close())
		p.target = nil
	}
	return err
}

func ExecveatFD(fd int, argv, env []string) error {
	if fd < 3 || len(argv) == 0 {
		return fmt.Errorf("invalid descriptor exec request")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return err
	}
	empty, err := syscall.BytePtrFromString("")
	if err != nil {
		return err
	}
	argvp, err := syscall.SlicePtrFromStrings(argv)
	if err != nil {
		return err
	}
	envp, err := syscall.SlicePtrFromStrings(env)
	if err != nil {
		return err
	}
	var argvPtr, envPtr uintptr
	if len(argvp) > 0 {
		argvPtr = uintptr(unsafe.Pointer(&argvp[0]))
	}
	if len(envp) > 0 {
		envPtr = uintptr(unsafe.Pointer(&envp[0]))
	}
	_, _, errno := unix.RawSyscall6(unix.SYS_EXECVEAT, uintptr(fd), uintptr(unsafe.Pointer(empty)), argvPtr, envPtr, uintptr(unix.AT_EMPTY_PATH), 0)
	runtime.KeepAlive(empty)
	runtime.KeepAlive(argvp)
	runtime.KeepAlive(envp)
	if errno != 0 {
		return errno
	}
	return fmt.Errorf("execveat returned unexpectedly")
}
