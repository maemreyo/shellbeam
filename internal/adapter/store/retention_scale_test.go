package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func retainedSessionCount(t *testing.T, r *Repository) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// TestRetentionBoundsTheRetainedCorpus is what retention is for: history stops
// growing without limit, so the one scan that remains linear -- startup
// reconciliation -- reads a bounded corpus rather than everything the daemon
// has ever run.
//
// This does not make reconciliation constant time. It bounds what it has to
// read, which is a different and weaker claim.
func TestRetentionBoundsTheRetainedCorpus(t *testing.T) {
	r := retentionRepository(t)
	const (
		expired = 200
		recent  = 20
	)
	for i := 0; i < expired; i++ {
		seedTerminal(t, r, i, 48*time.Hour)
	}
	for i := expired; i < expired+recent; i++ {
		seedTerminal(t, r, i, time.Minute)
	}
	if got := retainedSessionCount(t, r); got != expired+recent {
		t.Fatalf("seeded %d sessions, found %d", expired+recent, got)
	}

	collected := 0
	for pass := 0; pass < 16; pass++ {
		report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{
			TerminalRetention: time.Hour, MaxDeletions: 128,
		})
		if err != nil {
			t.Fatal(err)
		}
		collected += report.Collected
		if !report.Remaining {
			break
		}
	}
	if collected != expired {
		t.Fatalf("collected %d of %d expired sessions", collected, expired)
	}
	if got := retainedSessionCount(t, r); got != recent {
		t.Fatalf("retained corpus holds %d sessions, want the %d inside the window", got, recent)
	}

	// The index still agrees with the store after all that deletion.
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	active, _, err := r.admissionCounters()
	if err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("admission counts %d active sessions across a fully terminal corpus", active)
	}
}
