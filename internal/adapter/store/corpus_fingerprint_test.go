package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// TestRealCorpusRequestFingerprintsStillRecompute checks the compatibility
// promise against the operations that actually exist, not against examples.
//
// A reservation is replayed by comparing request fingerprints, so if adding
// stdin and timeout policy shifted the hash of a request that named neither,
// every stored operation would begin conflicting with itself. Set
// SHELLBEAM_CORPUS_STATE to a copy of a real state directory.
func TestRealCorpusRequestFingerprintsStillRecompute(t *testing.T) {
	root := os.Getenv("SHELLBEAM_CORPUS_STATE")
	if root == "" {
		t.Skip("SHELLBEAM_CORPUS_STATE not set")
	}
	entries, err := os.ReadDir(filepath.Join(root, "operations"))
	if err != nil {
		t.Fatal(err)
	}

	checked, skipped := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var stored operation.Reservation
		if err := readStrict(filepath.Join(root, "operations", entry.Name()), &stored); err != nil {
			// Records this build cannot decode say nothing about fingerprints.
			skipped++
			continue
		}
		recomputed, ok := recomputeRequestFingerprint(stored)
		if !ok {
			skipped++
			continue
		}
		if want := stored.EffectiveRequestFingerprint(); recomputed != want {
			t.Fatalf("%s: request fingerprint changed\n stored %s\n  now   %s\n reservation %s",
				entry.Name(), want, recomputed, mustJSON(t, stored))
		}
		checked++
	}
	t.Logf("recomputed %d stored request fingerprints unchanged (%d skipped)", checked, skipped)
	if checked == 0 {
		t.Fatal("no stored operation could be checked; the corpus proves nothing")
	}
}

// recomputeRequestFingerprint rebuilds the request the reservation recorded and
// hashes it with the current code.
func recomputeRequestFingerprint(stored operation.Reservation) (string, bool) {
	intent := operation.Intent{
		Command: stored.Command, Argv: stored.Argv, WorkspaceID: stored.WorkspaceID,
		CWD: stored.CWD, TTY: stored.TTY, TimeoutMS: stored.TimeoutMS,
		Persistent: stored.Persistent, SessionName: stored.SessionName,
	}
	if stored.LogicalCWD != "" {
		intent.CWD = stored.LogicalCWD
	}
	if stored.SchemaVersion == 1 {
		got, err := intent.Fingerprint()
		return got, err == nil
	}
	got, err := intent.RequestFingerprint()
	return got, err == nil
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(b)
}
