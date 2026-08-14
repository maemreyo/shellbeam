//go:build linux || darwin

package ipc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type socketDialer func(string, time.Duration) (net.Conn, error)

func prepareRuntime(runtime string) error {
	if !filepath.IsAbs(runtime) {
		return fmt.Errorf("runtime path must be absolute")
	}
	if err := os.MkdirAll(runtime, 0700); err != nil {
		return err
	}
	if err := os.Chmod(runtime, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(runtime)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe runtime directory")
	}
	return nil
}

type unixListenerFactory func(string) (net.Listener, error)

func claimSocket(socket string, dial socketDialer) (net.Listener, os.FileInfo, error) {
	return claimSocketWithListener(socket, dial, func(path string) (net.Listener, error) {
		return net.Listen("unix", path)
	})
}

func claimSocketWithListener(socket string, dial socketDialer, listen unixListenerFactory) (net.Listener, os.FileInfo, error) {
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("unsafe socket collision")
		}
		if err := removeStaleSocket(socket, info, dial); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	staged, err := stagedSocketPath(socket)
	if err != nil {
		return nil, nil, err
	}
	ln, err := listen(staged)
	if err != nil {
		return nil, nil, err
	}
	if unixListener, ok := ln.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	cleanup := func() {
		_ = ln.Close()
		_ = os.Remove(staged)
	}
	if err = os.Chmod(staged, 0600); err != nil {
		cleanup()
		return nil, nil, err
	}
	stagedInfo, err := os.Lstat(staged)
	if err != nil || stagedInfo.Mode()&os.ModeSocket == 0 || !ownedByCurrent(stagedInfo) {
		cleanup()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("unsafe staged socket")
	}
	if err = os.Link(staged, socket); err != nil {
		cleanup()
		if errors.Is(err, os.ErrExist) {
			return nil, nil, fmt.Errorf("daemon_already_running")
		}
		return nil, nil, err
	}
	info, err := os.Lstat(socket)
	if err != nil || !os.SameFile(stagedInfo, info) {
		_ = os.Remove(socket)
		cleanup()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("socket publication identity mismatch")
	}
	if err = os.Remove(staged); err != nil {
		_ = os.Remove(socket)
		cleanup()
		return nil, nil, err
	}
	return ln, info, nil
}

func stagedSocketPath(socket string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(socket), ".daemon.sock.pending-"+hex.EncodeToString(nonce[:])), nil
}

func removeStaleSocket(socket string, expected os.FileInfo, dial socketDialer) error {
	conn, err := dial(socket, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon_already_running")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("daemon_already_running")
	}
	current, err := os.Lstat(socket)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !os.SameFile(expected, current) {
		return fmt.Errorf("daemon_already_running")
	}
	return os.Remove(socket)
}

func dialUnixSocket(socket string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socket, timeout)
}
