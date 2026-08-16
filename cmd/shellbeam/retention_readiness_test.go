//go:build linux || darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedExpiredTerminalSessions writes a backlog of finished sessions whose
// durable terminal records are old enough to collect, without going through the
// daemon: the point is to have history waiting before it starts.
func seedExpiredTerminalSessions(t *testing.T, stateDir string, count int) {
	t.Helper()
	// Older than the default retention window of one week, so a daemon running
	// on stock configuration is expected to collect them.
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for _, dir := range []string{"operations", "sessions"} {
		if err := os.MkdirAll(filepath.Join(stateDir, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		operationID := fmt.Sprintf("expired-op-%04d", i)
		sessionID := fmt.Sprintf("expired-session-%04d", i)
		sessionDir := filepath.Join(stateDir, "sessions", sessionID)
		if err := os.MkdirAll(sessionDir, 0700); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, filepath.Join(stateDir, "operations", operationID+".json"), map[string]any{
			"schema_version": 1, "operation_id": operationID, "session_id": sessionID,
			"request_fingerprint": "fp", "command": "true", "cwd": "/tmp",
			"shell": "/bin/sh", "daemon_incarnation": "seed", "created_at": stale,
			"control_reservation_bytes": 0,
		})
		writeJSON(t, filepath.Join(sessionDir, "metadata.json"), map[string]any{
			"schema_version": 1, "operation_id": operationID, "session_id": sessionID,
			"daemon_incarnation": "seed", "state": "completed", "outcome": "success",
			"output_bytes": 0, "output_available": true, "updated_at": stale,
		})
		writeJSON(t, filepath.Join(sessionDir, "receipt.json"), map[string]any{
			"schema_version": 1, "operation_id": operationID, "session_id": sessionID,
			"fingerprint": "fp", "daemon_incarnation": "seed", "state": "completed",
			"outcome": "success", "output_complete": true,
			"spawn": map[string]any{"attempted": true, "succeeded": true},
			"exit":  map[string]any{"reaped": true, "code": 0},
		})
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func sessionCount(t *testing.T, stateDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stateDir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// TestReadinessDoesNotWaitForRetention.
//
// Collecting expired history is housekeeping, and an agent's first command must
// not queue behind it. A daemon inheriting a large backlog has to serve
// immediately and clean up behind itself -- the opposite arrangement would make
// every restart of a busy store cost an unbounded wait before anything could
// run.
func TestReadinessDoesNotWaitForRetention(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	seedExpiredTerminalSessions(t, stateDir, 300)
	if got := sessionCount(t, stateDir); got != 300 {
		t.Fatalf("seeded %d sessions", got)
	}

	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	// Serving already, with the backlog still there or already shrinking --
	// either way the daemon did not make the caller wait for it.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if sessionCount(t, stateDir) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the backlog was not collected behind readiness; %d sessions remain", sessionCount(t, stateDir))
}

// TestRetentionDisabledLeavesHistoryAlone at the daemon level: an operator who
// has not configured a window keeps everything.
func TestRetentionDisabledLeavesHistoryAlone(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	seedExpiredTerminalSessions(t, stateDir, 20)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("schema_version = 1\nterminal_retention_hours = 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	daemon := launchDaemonWithConfig(t, stateDir, runtimes[0], configPath)
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	// Give a sweep every chance to run before concluding it did not.
	time.Sleep(2 * time.Second)
	if got := sessionCount(t, stateDir); got != 20 {
		t.Fatalf("retention was disabled but history dropped to %d sessions", got)
	}
}
