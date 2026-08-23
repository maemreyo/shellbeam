//go:build darwin

package delegatedtmux

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

const processExitCancelIdent = uint64(0x53424831)

var errProcessAlreadyGone = errors.New("process already gone")

type processExitWatcher struct {
	kq     int
	mu     sync.Mutex
	closed bool
}

func newProcessExitWatcher(pid int) (*processExitWatcher, error) {
	if pid <= 1 {
		return nil, fmt.Errorf("invalid process pid")
	}
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	w := &processExitWatcher{kq: kq}
	changes := []unix.Kevent_t{
		{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT},
		{Ident: processExitCancelIdent, Filter: unix.EVFILT_USER, Flags: unix.EV_ADD | unix.EV_CLEAR},
	}
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		_ = w.Close()
		if errors.Is(err, unix.ESRCH) {
			return nil, errProcessAlreadyGone
		}
		return nil, err
	}
	return w, nil
}

func (w *processExitWatcher) Wait(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("nil process exit watcher")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = w.triggerCancel()
		case <-cancelDone:
		}
	}()
	defer close(cancelDone)
	events := make([]unix.Kevent_t, 2)
	for {
		n, err := unix.Kevent(w.kq, nil, events, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if retryProcessWaitError(err) {
				continue
			}
			return err
		}
		for _, event := range events[:n] {
			if event.Filter == unix.EVFILT_USER && event.Ident == processExitCancelIdent {
				if err := ctx.Err(); err != nil {
					return err
				}
				continue
			}
			if event.Filter == unix.EVFILT_PROC && event.Fflags&unix.NOTE_EXIT != 0 {
				return nil
			}
			if event.Flags&unix.EV_ERROR != 0 && event.Data != 0 {
				return unix.Errno(event.Data)
			}
		}
	}
}

func retryProcessWaitError(err error) bool {
	return errors.Is(err, unix.EINTR)
}

func (w *processExitWatcher) triggerCancel() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	change := unix.Kevent_t{Ident: processExitCancelIdent, Filter: unix.EVFILT_USER, Fflags: unix.NOTE_TRIGGER}
	_, err := unix.Kevent(w.kq, []unix.Kevent_t{change}, nil, nil)
	return err
}

func (w *processExitWatcher) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return unix.Close(w.kq)
}
