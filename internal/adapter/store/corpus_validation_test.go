package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// TestAgainstRealCorpus validates the index against a copy of a production
// state store. Set SHELLBEAM_CORPUS_STATE to a copy of a real state directory.
func TestAgainstRealCorpus(t *testing.T) {
	root := os.Getenv("SHELLBEAM_CORPUS_STATE")
	if root == "" {
		t.Skip("SHELLBEAM_CORPUS_STATE not set")
	}
	r, err := Open(root, Limits{
		MaxSessions: 4, MaxSessionOutput: 1 << 28, MaxTotalState: 1 << 34, ControlReserve: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	reconcile := time.Since(start)
	r.admissionMu.Lock()
	led := r.admission
	r.admissionMu.Unlock()
	t.Logf("reconcile: %s -> active=%d state_bytes=%d", reconcile.Round(time.Millisecond), led.ActiveSessions, led.StateBytes)

	scanActive, scanBytes, err := r.usage()
	if err != nil {
		t.Fatal(err)
	}
	if scanActive != led.ActiveSessions || scanBytes != led.StateBytes {
		t.Fatalf("index (%d, %d) disagrees with scan (%d, %d)", led.ActiveSessions, led.StateBytes, scanActive, scanBytes)
	}

	// Admission against the full corpus must be constant-cost.
	scansBefore := r.fullScans.Load()
	start = time.Now()
	const probes = 50
	admitted := 0
	for i := 0; i < probes; i++ {
		res := reservationN(900000 + i)
		_, created, got := r.ReserveOperation(context.Background(), res)
		if got.Err != nil {
			if got.Err.Error() != "capacity_exceeded" {
				t.Fatal(got.Err)
			}
			continue
		}
		if created {
			admitted++
			if pub := r.PublishTerminal(context.Background(), completedReceipt(res)); pub.Err != nil {
				t.Fatal(pub.Err)
			}
			if got := r.Compact(context.Background(), operation.SessionID(res.SessionID)); got.Err != nil {
				t.Fatal(got.Err)
			}
		}
	}
	elapsed := time.Since(start)
	sessions, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d admissions against a %d-session corpus: %s total, %s each, admitted=%d",
		probes, len(sessions), elapsed.Round(time.Millisecond),
		(elapsed / probes).Round(time.Microsecond), admitted)
	if got := r.fullScans.Load(); got != scansBefore {
		t.Fatalf("admission against real corpus performed %d full-tree scans", got-scansBefore)
	}
}
