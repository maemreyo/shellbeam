//go:build darwin || linux

package delegatedtmux

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func ensureRuntimeSocket(base, ref string) (string, error) {
	if !filepath.IsAbs(base) || !safeOpaque(ref, 128) {
		return "", fmt.Errorf("invalid delegated tmux runtime")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolvedBase)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid delegated tmux runtime base")
	}
	sum := sha256.Sum256([]byte(ref))
	dir := filepath.Join(resolvedBase, fmt.Sprintf("sb-%d", os.Getuid()), hex.EncodeToString(sum[:8]))
	if err := ensurePrivateDirectory(filepath.Dir(dir)); err != nil {
		return "", err
	}
	if err := ensurePrivateDirectory(dir); err != nil {
		return "", err
	}
	socket := filepath.Join(dir, "tmux.sock")
	if len(socket) >= 100 {
		return "", fmt.Errorf("delegated tmux socket path too long")
	}
	return socket, nil
}

func ensurePrivateDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("private directory must be absolute")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("unsafe private directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("unsafe private directory owner")
	}
	return nil
}
