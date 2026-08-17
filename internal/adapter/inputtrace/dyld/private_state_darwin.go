//go:build darwin

package dyld

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func providerRoot(stateDir string) string { return filepath.Join(stateDir, "input-trace", "dyld-v1") }
func artifactRoot(stateDir string) string { return filepath.Join(providerRoot(stateDir), "artifacts") }
func tracesRoot(stateDir string) string   { return filepath.Join(providerRoot(stateDir), "traces") }
func socketRoot() string {
	return filepath.Join("/tmp", "shellbeam-e27-"+strconv.Itoa(os.Geteuid()))
}

func ensureSocketRoot() (string, error) {
	root := socketRoot()
	if err := ensurePrivateDir(root); err != nil {
		return "", err
	}
	return root, nil
}

func validatePrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("unsafe private directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe private directory owner")
	}
	return nil
}

func validatePrivateRegular(path string, allowEmpty bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return fmt.Errorf("unsafe private file")
	}
	if !allowEmpty && info.Size() == 0 {
		return fmt.Errorf("empty private file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe private file owner")
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return validatePrivateDir(path)
}

func ensureProviderLayout(stateDir string) error {
	if err := validatePrivateDir(stateDir); err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(stateDir, "input-trace"), providerRoot(stateDir), artifactRoot(stateDir), tracesRoot(stateDir)} {
		if err := ensurePrivateDir(path); err != nil {
			return err
		}
	}
	return nil
}

func validateExistingProviderLayout(stateDir string) error {
	if err := validatePrivateDir(stateDir); err != nil {
		return err
	}
	paths := []string{filepath.Join(stateDir, "input-trace"), providerRoot(stateDir), artifactRoot(stateDir), tracesRoot(stateDir)}
	for _, path := range paths {
		err := validatePrivateDir(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}
