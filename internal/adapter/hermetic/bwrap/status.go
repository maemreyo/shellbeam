package bwrap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	maxProviderStatusLineBytes  = 8 << 10
	maxProviderStatusTotalBytes = 64 << 10
)

type providerStatusSnapshot struct {
	ChildPID int
	ExitCode *int
	CleanEOF bool
	Err      error
}

type providerStatusMonitor struct {
	reader io.ReadCloser
	ready  chan struct{}
	done   chan struct{}
	mu     sync.RWMutex
	snap   providerStatusSnapshot
	once   sync.Once
}

func newProviderStatusMonitor(reader io.ReadCloser) *providerStatusMonitor {
	m := &providerStatusMonitor{reader: reader, ready: make(chan struct{}), done: make(chan struct{})}
	go m.read()
	return m
}

func (m *providerStatusMonitor) read() {
	defer close(m.done)
	defer m.reader.Close()
	scanner := bufio.NewScanner(m.reader)
	scanner.Buffer(make([]byte, 1024), maxProviderStatusLineBytes)
	total := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		total += len(line)
		if total > maxProviderStatusTotalBytes {
			m.fail(fmt.Errorf("hermetic provider status budget exceeded"))
			return
		}
		if err := m.consume(line); err != nil {
			m.fail(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		m.fail(err)
		return
	}
	m.mu.Lock()
	m.snap.CleanEOF = true
	m.mu.Unlock()
}

func (m *providerStatusMonitor) consume(line []byte) error {
	var object map[string]json.RawMessage
	if len(line) == 0 || json.Unmarshal(line, &object) != nil || object == nil {
		return fmt.Errorf("invalid hermetic provider status")
	}
	if raw, ok := object["child-pid"]; ok {
		var pid int
		if json.Unmarshal(raw, &pid) != nil || pid <= 0 {
			return fmt.Errorf("invalid hermetic provider child pid")
		}
		m.mu.Lock()
		if m.snap.ChildPID != 0 && m.snap.ChildPID != pid {
			m.mu.Unlock()
			return fmt.Errorf("hermetic provider child pid changed")
		}
		m.snap.ChildPID = pid
		m.mu.Unlock()
		m.once.Do(func() { close(m.ready) })
	}
	if raw, ok := object["exit-code"]; ok {
		var code int
		if json.Unmarshal(raw, &code) != nil || code < 0 || code > 255 {
			return fmt.Errorf("invalid hermetic provider exit code")
		}
		m.mu.Lock()
		if m.snap.ExitCode != nil && *m.snap.ExitCode != code {
			m.mu.Unlock()
			return fmt.Errorf("hermetic provider exit code changed")
		}
		copy := code
		m.snap.ExitCode = &copy
		m.mu.Unlock()
	}
	return nil
}

func (m *providerStatusMonitor) fail(err error) {
	m.mu.Lock()
	if m.snap.Err == nil {
		m.snap.Err = err
	}
	m.mu.Unlock()
}

func (m *providerStatusMonitor) awaitReady(ctx context.Context, budget time.Duration) error {
	if m == nil || budget <= 0 {
		return fmt.Errorf("hermetic provider status unavailable")
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-m.ready:
		return nil
	case <-m.done:
		snap := m.snapshot()
		if snap.ChildPID > 0 {
			return nil
		}
		if snap.Err != nil {
			return snap.Err
		}
		return io.ErrUnexpectedEOF
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("hermetic provider startup proof timeout")
	}
}

func (m *providerStatusMonitor) awaitDone(budget time.Duration) providerStatusSnapshot {
	if m == nil {
		return providerStatusSnapshot{Err: errors.New("hermetic provider status unavailable")}
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-m.done:
		return m.snapshot()
	case <-timer.C:
		_ = m.reader.Close()
		<-m.done
		snap := m.snapshot()
		if snap.Err == nil {
			snap.Err = errors.New("hermetic provider terminal status timeout")
		}
		return snap
	}
}

func (m *providerStatusMonitor) snapshot() providerStatusSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.snap
	if m.snap.ExitCode != nil {
		code := *m.snap.ExitCode
		out.ExitCode = &code
	}
	return out
}
