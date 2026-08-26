//go:build darwin

package contextexec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
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
	child, err := spawnSuspendedDarwinChild(p.targetPath, p.spec)
	if err != nil {
		return nil, err
	}
	verify := p.launcher.verifyMappedExecutable
	if verify == nil {
		verify = verifyDarwinMappedExecutable
	}
	if err := verify(child.pid, p.identity); err != nil {
		child.failClosed()
		return nil, err
	}
	if err := child.resume(); err != nil {
		child.failClosed()
		return nil, err
	}
	return child.publicProcess(p.targetPath), nil
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

func ExecveatFD(int, []string, []string) error { return fmt.Errorf("execveat unavailable on darwin") }
