//go:build linux || darwin

package process

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"sync"
	"testing"
	"time"
)

type memorySink struct {
	mu  sync.Mutex
	b   []byte
	err error
}

func (s *memorySink) Append(_ context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = append(s.b, b...)
	return s.err
}
func (s *memorySink) CaptureFailed(err error) { s.mu.Lock(); s.err = err; s.mu.Unlock() }

func TestOwnerRunsAndCaptures(t *testing.T) {
	sink := &memorySink{}
	h, spawn, err := (Owner{}).Start(context.Background(), operation.ExecutionSpec{Shell: "/bin/sh", Command: "printf out; printf err >&2", CWD: t.TempDir()}, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	sink.mu.Lock()
	got := string(sink.b)
	sink.mu.Unlock()
	if got != "outerr" && got != "errout" {
		t.Fatalf("output=%q", got)
	}
}
func TestRequestCancellationDoesNotKill(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sink := &memorySink{}
	h, _, err := (Owner{}).Start(ctx, operation.ExecutionSpec{Shell: "/bin/sh", Command: "sleep 0.05; printf alive", CWD: t.TempDir()}, sink)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() { h.Wait(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	sink.mu.Lock()
	got := string(sink.b)
	sink.mu.Unlock()
	if got != "alive" {
		t.Fatalf("output=%q", got)
	}
}

func TestOwnerPTY(t *testing.T) {
	sink := &memorySink{}
	h, spawn, err := (Owner{}).Start(context.Background(), operation.ExecutionSpec{Shell: "/bin/sh", Command: "[ -t 0 ] && printf tty", CWD: t.TempDir(), TTY: true}, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	e := h.Wait(context.Background())
	if !e.Reaped {
		t.Fatalf("exit=%#v", e)
	}
	sink.mu.Lock()
	got := string(sink.b)
	sink.mu.Unlock()
	if got != "tty" {
		t.Fatalf("output=%q", got)
	}
}
