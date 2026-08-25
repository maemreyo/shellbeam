//go:build darwin

package contextexec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	darwinProcInfoCallPIDInfo       = 2
	darwinProcPIDRegionPathInfo     = 8
	darwinProcRegionWithPathInfoLen = 1272
	darwinVMProtExecute             = 4
	darwinRegionProtectionOffset    = 0
	darwinRegionAddressOffset       = 80
	darwinRegionSizeOffset          = 88
	darwinVnodeStatOffset           = 96
	darwinVnodeDeviceOffset         = darwinVnodeStatOffset
	darwinVnodeInodeOffset          = darwinVnodeStatOffset + 8
	darwinVnodeGenerationOffset     = darwinVnodeStatOffset + 112
	darwinMaxRegionScan             = 4096
)

type darwinExecutableIdentity struct {
	Device     uint32
	Inode      uint64
	Generation uint32
}

type darwinPlatformLauncher struct {
	verifyMappedExecutable func(int, darwinExecutableIdentity) error
}

func NewPlatformLauncher(_ ...string) ChildLauncher { return darwinPlatformLauncher{} }
func (darwinPlatformLauncher) Qualified() bool      { return true }

func (l darwinPlatformLauncher) Prepare(spec ChildSpec) (PreparedExecution, error) {
	targetPath, err := resolveChildExecutable(spec)
	if err != nil {
		return nil, err
	}
	target, err := os.Open(targetPath)
	if err != nil {
		return nil, err
	}
	identity, err := openedDarwinExecutableIdentity(target)
	if err != nil {
		_ = target.Close()
		return nil, err
	}
	return &darwinPreparedExecution{launcher: l, spec: spec, targetPath: targetPath, target: target, identity: identity}, nil
}

type darwinPreparedExecution struct {
	launcher   darwinPlatformLauncher
	spec       ChildSpec
	targetPath string
	target     *os.File
	identity   darwinExecutableIdentity
	mu         sync.Mutex
	started    bool
	closed     bool
}

func (p *darwinPreparedExecution) ResolvedExecutable() string {
	if p == nil {
		return ""
	}
	return p.targetPath
}

func (p *darwinPreparedExecution) Start() (*ChildProcess, error) {
	if p == nil {
		return nil, fmt.Errorf("prepared context executable unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.started || p.target == nil {
		return nil, fmt.Errorf("prepared context executable is not startable")
	}
	p.started = true
	cmd := &exec.Cmd{
		Path: p.targetPath,
		Args: append([]string(nil), p.spec.Argv...),
		Dir:  p.spec.CWD,
		Env:  append([]string(nil), p.spec.Env...),
		SysProcAttr: &syscall.SysProcAttr{
			Ptrace:  true,
			Setpgid: true,
		},
	}
	pipes, err := attachChildOutputPipes(cmd)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	if err := pipes.closeParentWriters(); err != nil {
		failClosedDarwinChild(cmd, pipes.stdoutR, pipes.stderrR)
		return nil, fmt.Errorf("close parent context child output writers: %w", err)
	}
	if err := waitForDarwinExecStop(cmd.Process.Pid); err != nil {
		failClosedDarwinChild(cmd, pipes.stdoutR, pipes.stderrR)
		return nil, err
	}
	verify := p.launcher.verifyMappedExecutable
	if verify == nil {
		verify = verifyDarwinMappedExecutable
	}
	if err := verify(cmd.Process.Pid, p.identity); err != nil {
		failClosedDarwinChild(cmd, pipes.stdoutR, pipes.stderrR)
		return nil, err
	}
	if err := resumeAndDetachDarwinChild(cmd.Process.Pid); err != nil {
		failClosedDarwinChild(cmd, pipes.stdoutR, pipes.stderrR)
		return nil, err
	}
	wait := darwinChildWaiter(cmd)
	kill := func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return &ChildProcess{ResolvedExecutable: p.targetPath, Stdout: pipes.stdoutR, Stderr: pipes.stderrR, Wait: wait, KillGroup: kill}, nil
}

func (p *darwinPreparedExecution) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.target == nil {
		return nil
	}
	err := p.target.Close()
	p.target = nil
	return err
}

func openedDarwinExecutableIdentity(target *os.File) (darwinExecutableIdentity, error) {
	info, err := target.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return darwinExecutableIdentity{}, fmt.Errorf("invalid opened context executable")
	}
	var prefix [2]byte
	n, err := target.ReadAt(prefix[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return darwinExecutableIdentity{}, err
	}
	if n < len(prefix) {
		return darwinExecutableIdentity{}, fmt.Errorf("invalid opened context executable")
	}
	if bytes.Equal(prefix[:], []byte("#!")) {
		return darwinExecutableIdentity{}, fmt.Errorf("context executable interpreter chain is unqualified")
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(target.Fd()), &stat); err != nil {
		return darwinExecutableIdentity{}, err
	}
	return darwinExecutableIdentity{Device: uint32(stat.Dev), Inode: stat.Ino, Generation: stat.Gen}, nil
}

func waitForDarwinExecStop(pid int) error {
	var status syscall.WaitStatus
	got, err := syscall.Wait4(pid, &status, syscall.WUNTRACED, nil)
	if err != nil {
		return fmt.Errorf("wait for traced context child: %w", err)
	}
	if got != pid || !status.Stopped() || status.StopSignal() != syscall.SIGTRAP {
		return fmt.Errorf("context child did not stop at exec boundary")
	}
	return nil
}

// resumeAndDetachDarwinChild suppresses the exec-boundary SIGTRAP before
// detaching. Newer Darwin kernels can otherwise re-deliver that trap after
// PT_DETACH and terminate the qualified workload before its first instruction.
// Queueing SIGSTOP while the tracee is already stopped gives us a second
// kernel boundary: PT_CONTINUE suppresses SIGTRAP, the pending SIGSTOP is
// observed before user-space resumes, and PT_DETACH can then release the child.
func resumeAndDetachDarwinChild(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return fmt.Errorf("queue qualified context child stop: %w", err)
	}
	if err := ptraceContinueDarwin(pid, 0); err != nil {
		return fmt.Errorf("suppress context child exec trap: %w", err)
	}
	var status syscall.WaitStatus
	got, err := syscall.Wait4(pid, &status, syscall.WUNTRACED, nil)
	if err != nil {
		return fmt.Errorf("wait for qualified context child stop: %w", err)
	}
	// On BSD/Darwin syscall.WaitStatus reports a SIGSTOP wait record through
	// Continued(), not Stopped(); the raw record is still the required kernel
	// stop boundary for ptrace detachment.
	if got != pid || !status.Continued() {
		return fmt.Errorf("context child did not reach detach boundary")
	}
	if err := syscall.PtraceDetach(pid); err != nil {
		return fmt.Errorf("detach qualified context child: %w", err)
	}
	if err := syscall.Kill(pid, syscall.SIGCONT); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("resume detached context child: %w", err)
	}
	return nil
}

func ptraceContinueDarwin(pid int, signal syscall.Signal) error {
	_, _, errno := syscall.RawSyscall6(
		syscall.SYS_PTRACE, uintptr(syscall.PT_CONTINUE), uintptr(pid), uintptr(1), uintptr(signal), 0, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func verifyDarwinMappedExecutable(pid int, expected darwinExecutableIdentity) error {
	var address uint64
	for scanned := 0; scanned < darwinMaxRegionScan; scanned++ {
		region, err := readDarwinRegion(pid, address)
		if err != nil {
			return fmt.Errorf("observe mapped context executable: %w", err)
		}
		protection := binary.LittleEndian.Uint32(region[darwinRegionProtectionOffset : darwinRegionProtectionOffset+4])
		regionAddress := binary.LittleEndian.Uint64(region[darwinRegionAddressOffset : darwinRegionAddressOffset+8])
		regionSize := binary.LittleEndian.Uint64(region[darwinRegionSizeOffset : darwinRegionSizeOffset+8])
		if protection&darwinVMProtExecute != 0 {
			observed := darwinExecutableIdentity{
				Device:     binary.LittleEndian.Uint32(region[darwinVnodeDeviceOffset : darwinVnodeDeviceOffset+4]),
				Inode:      binary.LittleEndian.Uint64(region[darwinVnodeInodeOffset : darwinVnodeInodeOffset+8]),
				Generation: binary.LittleEndian.Uint32(region[darwinVnodeGenerationOffset : darwinVnodeGenerationOffset+4]),
			}
			if observed != expected {
				return fmt.Errorf("mapped context executable identity mismatch")
			}
			return nil
		}
		next := regionAddress + regionSize
		if regionSize == 0 || next <= address {
			return fmt.Errorf("invalid context executable mapping progression")
		}
		address = next
	}
	return fmt.Errorf("mapped context executable not found")
}

func readDarwinRegion(pid int, address uint64) ([]byte, error) {
	region := make([]byte, darwinProcRegionWithPathInfoLen)
	returned, _, errno := syscall.RawSyscall6(
		syscall.SYS_PROC_INFO,
		uintptr(darwinProcInfoCallPIDInfo),
		uintptr(pid),
		uintptr(darwinProcPIDRegionPathInfo),
		uintptr(address),
		uintptr(unsafe.Pointer(&region[0])),
		uintptr(len(region)),
	)
	runtime.KeepAlive(region)
	if errno != 0 {
		return nil, errno
	}
	if returned != uintptr(len(region)) {
		return nil, fmt.Errorf("short proc region observation: %d", returned)
	}
	return region, nil
}

func failClosedDarwinChild(cmd *exec.Cmd, stdout, stderr io.ReadCloser) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = syscall.PtraceDetach(cmd.Process.Pid)
		_, _ = cmd.Process.Wait()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}
}

func darwinChildWaiter(cmd *exec.Cmd) func() (ChildExit, error) {
	var once sync.Once
	var exit ChildExit
	var waitErr error
	return func() (ChildExit, error) {
		once.Do(func() {
			err := cmd.Wait()
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					waitErr = err
					return
				}
			}
			exit.Reaped = true
			if code := cmd.ProcessState.ExitCode(); code >= 0 {
				exit.Code = &code
			}
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				exit.Signal = status.Signal().String()
			}
		})
		return exit, waitErr
	}
}

func ExecveatFD(int, []string, []string) error { return fmt.Errorf("execveat unavailable on darwin") }
