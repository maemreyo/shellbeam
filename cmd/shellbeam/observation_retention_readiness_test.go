//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedMaterializedObligationLedger writes a backlog of committed obligations
// and an event projection that has already absorbed all of them, which is the
// shape a long-running daemon accumulates and never used to shed.
func seedMaterializedObligationLedger(t *testing.T, stateDir string, count int) {
	t.Helper()
	dir := filepath.Join(stateDir, "observations", "obligations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	prepared := time.Now().UTC().Add(-24 * time.Hour)
	for seq := 1; seq <= count; seq++ {
		writeJSON(t, filepath.Join(dir, fmt.Sprintf("%020d.json", seq)), map[string]any{
			"schema_version": 1, "change_seq": seq, "kind": "operation_admitted",
			"state": "committed", "prepared_at": prepared,
			"correlation": map[string]any{"operation_id": "seed-op", "session_id": "seed-session"},
			"subject_ref": "operation:seed", "summary": "operation admitted",
		})
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "observations", "events"), 0700); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(stateDir, "observations", "projection-state.json"), map[string]any{
		"schema_version": 1, "materialized_through_seq": count, "compacted_through_seq": 0,
	})
}

func obligationCount(t *testing.T, stateDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stateDir, "observations", "obligations"))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// TestDaemonCollectsTheObservationLedger is a wiring test, and it is one on
// purpose.
//
// Every gap this change closed was a mechanism that had been written, validated
// and tested and then never called from anywhere -- min_free_space_bytes,
// CompactEvents. Collection that only works when a test invokes the repository
// directly would be the same bug again, so this asserts the ledger shrinks
// under a real daemon that nobody asked to collect anything.
func TestDaemonCollectsTheObservationLedger(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	seedMaterializedObligationLedger(t, stateDir, 400)
	if got := obligationCount(t, stateDir); got != 400 {
		t.Fatalf("seeded %d obligations", got)
	}

	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		// The newest record is pinned so a restart can still recover the high
		// watermark from it; everything below the projection goes.
		if obligationCount(t, stateDir) == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the ledger was not collected; %d obligations remain", obligationCount(t, stateDir))
}

// TestCollectedLedgerStillRestarts is the consequence that matters.
//
// The high watermark is recovered from the highest surviving obligation
// filename, so a sweep that collected the wrong record would leave a store that
// its own daemon can no longer open. Restarting onto the collected ledger is
// the only assertion that actually proves it did not.
func TestCollectedLedgerStillRestarts(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a", "run-b")
	seedMaterializedObligationLedger(t, stateDir, 200)

	first := launchDaemon(t, stateDir, runtimes[0])
	if !first.serving(t) {
		t.Fatalf("daemon never served: %s", first.output(t))
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && obligationCount(t, stateDir) > 1 {
		time.Sleep(100 * time.Millisecond)
	}
	if got := obligationCount(t, stateDir); got != 1 {
		t.Fatalf("ledger did not collect before restart; %d remain", got)
	}
	first.stop(t)

	second := launchDaemon(t, stateDir, runtimes[1])
	if !second.serving(t) {
		t.Fatalf("a daemon could not reopen its own collected ledger: %s", second.output(t))
	}
}
