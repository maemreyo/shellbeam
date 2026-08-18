//go:build linux

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/maemreyo/shellbeam/internal/core/capability"
)

type osCgroupOps struct{}

func (osCgroupOps) selfPID() int                        { return os.Getpid() }
func (osCgroupOps) resolve(path string) (string, error) { return filepath.EvalSymlinks(path) }
func (osCgroupOps) kind(path string) (resourcePathKind, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return pathMissing, err
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return pathSymlink, nil
	case mode.IsDir():
		return pathDirectory, nil
	case mode.IsRegular():
		return pathRegular, nil
	default:
		return pathOther, nil
	}
}
func (osCgroupOps) isCgroup2(path string) (bool, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false, err
	}
	return uint64(stat.Type) == uint64(unix.CGROUP2_SUPER_MAGIC), nil
}
func (ops osCgroupOps) readDir(path string) ([]resourceDirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]resourceDirEntry, 0, len(entries))
	for _, entry := range entries {
		kind := pathOther
		if entry.Type()&os.ModeSymlink != 0 {
			kind = pathSymlink
		} else if entry.IsDir() {
			kind = pathDirectory
		} else {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil, infoErr
			}
			if info.Mode().IsRegular() {
				kind = pathRegular
			}
		}
		out = append(out, resourceDirEntry{Name: entry.Name(), Kind: kind})
	}
	return out, nil
}
func (osCgroupOps) readFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (osCgroupOps) writeFile(path, value string) error {
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
func (osCgroupOps) mkdir(path string) error  { return os.Mkdir(path, 0o755) }
func (osCgroupOps) remove(path string) error { return os.Remove(path) }

func newResourceProviderFromEnvironment() (resourceProvider, *capability.ResourceEnforcementSupport, error) {
	root := strings.TrimSpace(os.Getenv(resourceCgroupRootEnv))
	if root == "" {
		return nil, nil, nil
	}
	provider, err := qualifyResourceProvider(root, osCgroupOps{}, newOpaqueResourceName)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyResourceAtomicPlacement(provider, runResourceAtomicPlacementProbe); err != nil {
		return nil, nil, err
	}
	support := provider.support()
	return provider, &support, nil
}

type linuxResourceSpawnBinding struct{ fd int }

func (b *linuxResourceSpawnBinding) Close() error {
	if b == nil || b.fd < 0 {
		return nil
	}
	err := unix.Close(b.fd)
	b.fd = -1
	return err
}

func bindResourceDomainToCommand(path string, cmd *exec.Cmd) (resourceSpawnBinding, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, resourceProviderFailure("cgroup_fd_open_failed")
	}
	binding := &linuxResourceSpawnBinding{fd: fd}
	if err := applyResourceCgroupFD(cmd, fd); err != nil {
		_ = binding.Close()
		return nil, err
	}
	return binding, nil
}

func applyResourceCgroupFD(cmd *exec.Cmd, fd int) error {
	if cmd == nil || fd < 0 {
		return resourceProviderFailure("cgroup_fd_invalid")
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = fd
	return nil
}

func runResourceAtomicPlacementProbe(domain resourceExecutionDomain) error {
	cmd := exec.Command("/bin/true")
	binding, err := domain.bind(cmd)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = binding.Close()
		return err
	}
	_ = binding.Close()
	domain.startMonitoring()
	return cmd.Wait()
}
