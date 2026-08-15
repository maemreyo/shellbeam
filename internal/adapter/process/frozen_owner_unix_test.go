//go:build linux || darwin

package process

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestFrozenOwnerUsesAdmittedExecutableWithoutPathRebinding(t *testing.T) {
	sink := &frozenOwnerSink{}
	handle, spawn, err := (FrozenOwner{}).Start(context.Background(), operation.ExecutionSpec{
		Mode: operation.ExecutionModeArgv, Executable: "/bin/echo",
		Argv: []string{"definitely-not-on-path", "frozen-ok"}, CWD: "/tmp",
	}, sink)
	if err != nil || !spawn.Attempted || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := handle.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	if got := sink.String(); !strings.Contains(got, "frozen-ok") {
		t.Fatalf("output=%q", got)
	}
}

func TestFrozenOwnerRejectsRelativeOrInconsistentFrozenExecutable(t *testing.T) {
	for _, spec := range []operation.ExecutionSpec{
		{Mode: operation.ExecutionModeArgv, Executable: "echo", Argv: []string{"echo", "x"}, CWD: "/tmp"},
		{Mode: operation.ExecutionModeShell, Executable: "/bin/sh", Shell: "/bin/bash", Command: "true", CWD: "/tmp"},
	} {
		if _, spawn, err := (FrozenOwner{}).Start(context.Background(), spec, &frozenOwnerSink{}); err == nil || !spawn.Attempted || spawn.Succeeded {
			t.Fatalf("invalid frozen spec accepted: %#v spawn=%#v err=%v", spec, spawn, err)
		}
	}
}

type frozenOwnerSink struct {
	mu   sync.Mutex
	data []byte
	err  error
}

func (s *frozenOwnerSink) Append(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, data...)
	return nil
}

func (s *frozenOwnerSink) CaptureFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *frozenOwnerSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.data)
}
