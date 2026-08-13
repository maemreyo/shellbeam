//go:build linux || darwin

package ipc

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const startupLockName = "daemon.startup.lock"

type startupLock struct {
	file *os.File
}

func acquireStartupLock(runtime string) (*startupLock, error) {
	path := filepath.Join(runtime, startupLockName)
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open startup lock")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !ownedByCurrent(info) {
		_ = file.Close()
		return nil, fmt.Errorf("unsafe startup lock")
	}
	if err := flockExclusive(fd); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &startupLock{file: file}, nil
}

func flockExclusive(fd int) error {
	for {
		err := unix.Flock(fd, unix.LOCK_EX)
		if err != unix.EINTR {
			return err
		}
	}
}

func (l *startupLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	fd := int(l.file.Fd())
	var unlockErr error
	for {
		unlockErr = unix.Flock(fd, unix.LOCK_UN)
		if unlockErr != unix.EINTR {
			break
		}
	}
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
