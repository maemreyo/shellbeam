//go:build linux || darwin

package process

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestArgvPreservesExactArgumentBoundaries(t *testing.T) {
	t.Setenv("SHELLBEAM_ARGV_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a b", "quoted", "*", "", "日本語", "--flag"}
	argv := append([]string{exe, "-test.run=TestArgvHelperProcess", "--"}, want...)
	sink := &memorySink{}
	h, spawn, err := (Owner{}).Start(context.Background(), operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: argv, CWD: t.TempDir()}, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	sink.mu.Lock()
	raw := append([]byte(nil), sink.b...)
	sink.mu.Unlock()
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("output=%q err=%v", raw, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestArgvHelperProcess(t *testing.T) {
	if os.Getenv("SHELLBEAM_ARGV_HELPER") != "1" {
		return
	}
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 {
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(os.Args[index+1:]); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestArgvPTYAndEnvironmentInheritance(t *testing.T) {
	t.Setenv("SHELLBEAM_ARGV_ENV", "inherited")
	sink := &memorySink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/sh", "-c", "[ -t 0 ] && printf %s \"$SHELLBEAM_ARGV_ENV\""}, CWD: t.TempDir(), TTY: true}
	h, spawn, err := (Owner{}).Start(context.Background(), spec, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	if exit := h.Wait(context.Background()); !exit.Reaped {
		t.Fatalf("exit=%#v", exit)
	}
	sink.mu.Lock()
	got := strings.TrimSuffix(string(sink.b), "\r")
	sink.mu.Unlock()
	if got != "inherited" {
		t.Fatalf("output=%q", got)
	}
}

func TestArgvRequestCancellationDoesNotKill(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sink := &memorySink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/sh", "-c", "sleep 0.05; printf alive"}, CWD: t.TempDir()}
	h, _, err := (Owner{}).Start(ctx, spec, sink)
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

func TestArgvMissingExecutableIsTypedSpawnFailure(t *testing.T) {
	sink := &memorySink{}
	spec := operation.ExecutionSpec{Mode: operation.ExecutionModeArgv, Argv: []string{"shellbeam-definitely-missing-executable"}, CWD: t.TempDir()}
	_, spawn, err := (Owner{}).Start(context.Background(), spec, sink)
	if err == nil || spawn.Succeeded || !spawn.Attempted || spawn.ErrorCode != "executable_not_found" {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
}
