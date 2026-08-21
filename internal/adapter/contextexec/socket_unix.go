//go:build darwin || linux

package contextexec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

const maxPrivateSocketPathBytes = 100

func privateSocketPath(runtimeDir, launchID string) (string, error) {
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) || !validOpaque(launchID, core.MaxOpaqueRefBytes) {
		return "", fmt.Errorf("invalid context helper socket identity")
	}
	sum := sha256.Sum256([]byte("shellbeam-context-exec-v1\x00" + launchID))
	name := ".cx-" + hex.EncodeToString(sum[:12]) + ".sock"
	path := filepath.Join(runtimeDir, name)
	if len(path) >= maxPrivateSocketPathBytes {
		return "", fmt.Errorf("context helper socket path too long")
	}
	return path, nil
}
func ListenPrivate(runtimeDir, launchID string) (net.Listener, string, error) {
	if err := verifyPrivateRuntimeDir(runtimeDir); err != nil {
		return nil, "", err
	}
	path, err := privateSocketPath(runtimeDir, launchID)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, "", fmt.Errorf("context helper socket collision")
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", err
	}
	cleanup := func() { _ = listener.Close(); _ = os.Remove(path) }
	if err := os.Chmod(path, 0600); err != nil {
		cleanup()
		return nil, "", err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !ownedByCurrentUser(info) {
		cleanup()
		return nil, "", fmt.Errorf("unsafe context helper socket")
	}
	return listener, path, nil
}
func DialPrivate(runtimeDir, launchID string) (net.Conn, error) {
	path, err := privateSocketPath(runtimeDir, launchID)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("unsafe context helper socket")
	}
	return net.Dial("unix", path)
}
func verifyPrivateRuntimeDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrentUser(info) {
		return fmt.Errorf("unsafe context helper runtime directory")
	}
	return nil
}
func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
