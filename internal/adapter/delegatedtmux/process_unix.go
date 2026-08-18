//go:build darwin || linux

package delegatedtmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func signalValue(name string) (syscall.Signal, error) {
	switch name {
	case "INT":
		return syscall.SIGINT, nil
	case "TERM":
		return syscall.SIGTERM, nil
	case "KILL":
		return syscall.SIGKILL, nil
	default:
		return 0, fmt.Errorf("unsupported signal")
	}
}
func signalProcessGroup(pid int, name string) error {
	if pid <= 1 {
		return fmt.Errorf("invalid pane pid")
	}
	sig, err := signalValue(name)
	if err != nil {
		return err
	}
	return syscall.Kill(-pid, sig)
}

func makePrivateFIFO(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("fifo path must be absolute")
	}
	_ = os.Remove(path)
	return syscall.Mkfifo(path, 0o600)
}

func releasePrivateFIFO(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("fifo path must be absolute")
	}
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			file := os.NewFile(uintptr(fd), path)
			_, writeErr := file.Write([]byte("go\n"))
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		if !errors.Is(err, syscall.ENXIO) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}
