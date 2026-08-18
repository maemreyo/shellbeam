//go:build linux

package process

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/maemreyo/shellbeam/internal/core/capability"
)

type osCgroupOps struct{}

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
	support := provider.support()
	return provider, &support, nil
}
