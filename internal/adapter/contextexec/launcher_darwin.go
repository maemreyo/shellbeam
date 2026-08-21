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
	afterOpen              func(string) error
	verifyMappedExecutable func(int, darwinExecutableIdentity) error
}

func NewPlatformLauncher(_ ...string) ChildLauncher { return darwinPlatformLauncher{} }
func (darwinPlatformLauncher) Qualified() bool      { return true }

func (l darwinPlatformLauncher) Launch(spec ChildSpec) (*ChildProcess, error) {
	targetPath, err := resolveChildExecutable(spec)
	if err != nil {
		return nil, err
	}
	target, err := os.Open(targetPath)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	identity, err := openedDarwinExecutableIdentity(target)
	if err != nil {
		return nil, err
	}
	if l.afterOpen != nil {
		if err := l.afterOpen(targetPath); err != nil {
			return nil, err
		}
	}

	cmd := &exec.Cmd{
		Path: targetPath,
		Args: append([]string(nil), spec.Argv...),
		Dir:  spec.CWD,
		Env:  append([]string(nil), spec.Env...),
		SysProcAttr: &syscall.SysProcAttr{
			Ptrace:  true,
			Setpgid: true,
		},
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	if err := waitForDarwinExecStop(cmd.Process.Pid); err != nil {
		failClosedDarwinChild(cmd, stdout, stderr)
		return nil, err
	}
	verify := l.verifyMappedExecutable
	if verify == nil {
		verify = verifyDarwinMappedExecutable
	}
	if err := verify(cmd.Process.Pid, identity); err != nil {
		failClosedDarwinChild(cmd, stdout, stderr)
		return nil, err
	}
	if err := syscall.PtraceDetach(cmd.Process.Pid); err != nil {
		failClosedDarwinChild(cmd, stdout, stderr)
		return nil, fmt.Errorf("detach qualified context child: %w", err)
	}

	wait := darwinChildWaiter(cmd)
	kill := func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return &ChildProcess{ResolvedExecutable: targetPath, Stdout: stdout, Stderr: stderr, Wait: wait, KillGroup: kill}, nil
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
