//go:build linux || darwin

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/observability"
)

func countEvents(log string, event string) int {
	return strings.Count(log, `"event":"`+event+`"`)
}

// TestFreeSpaceWatchWarnsOnceWhileSpaceStaysLow keeps the warning readable.
//
// A watch that re-announced the same condition on every pass would write a line
// every few minutes for as long as the disk stayed full, which buries the
// moment it actually crossed the floor under thousands of identical lines.
func TestFreeSpaceWatchWarnsOnceWhileSpaceStaysLow(t *testing.T) {
	var out bytes.Buffer
	available := int64(10)
	watch := newFreeSpaceWatch(100, func() (int64, error) { return available, nil }, observability.New(&out))

	watch.sample()
	watch.sample()
	watch.sample()

	if got := countEvents(out.String(), "free_space_low"); got != 1 {
		t.Fatalf("free_space_low events = %d, want 1: %s", got, out.String())
	}
}

// TestFreeSpaceWatchReportsRecovery closes the loop: an operator who freed room
// has to be told the condition cleared, or the last line they ever see about
// this disk is the alarm.
func TestFreeSpaceWatchReportsRecovery(t *testing.T) {
	var out bytes.Buffer
	available := int64(10)
	watch := newFreeSpaceWatch(100, func() (int64, error) { return available, nil }, observability.New(&out))

	watch.sample()
	available = 1000
	watch.sample()
	watch.sample()

	if got := countEvents(out.String(), "free_space_recovered"); got != 1 {
		t.Fatalf("free_space_recovered events = %d, want 1: %s", got, out.String())
	}
	// Falling below again is a new crossing and must be announced again.
	available = 10
	watch.sample()
	if got := countEvents(out.String(), "free_space_low"); got != 2 {
		t.Fatalf("free_space_low events after a second crossing = %d, want 2: %s", got, out.String())
	}
}

// TestFreeSpaceWatchStaysQuietWithRoomToSpare stops the daemon from writing
// anything at all on a healthy machine.
func TestFreeSpaceWatchStaysQuietWithRoomToSpare(t *testing.T) {
	var out bytes.Buffer
	watch := newFreeSpaceWatch(100, func() (int64, error) { return 1000, nil }, observability.New(&out))

	watch.sample()
	watch.sample()

	if out.Len() != 0 {
		t.Fatalf("watch wrote %q while the disk had room", out.String())
	}
}

// TestFreeSpaceWatchSurvivesAnUnreadableFilesystem. The watch is housekeeping;
// a probe that cannot answer must not take the daemon down with it, and must
// not be mistaken for a full disk either.
func TestFreeSpaceWatchSurvivesAnUnreadableFilesystem(t *testing.T) {
	var out bytes.Buffer
	watch := newFreeSpaceWatch(100, func() (int64, error) { return 0, fmt.Errorf("statfs refused") }, observability.New(&out))

	watch.sample()

	if got := countEvents(out.String(), "free_space_low"); got != 0 {
		t.Fatalf("an unreadable filesystem was reported as low on space: %s", out.String())
	}
}
