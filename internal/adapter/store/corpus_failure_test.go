package store

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

// TestRealCorpusFailuresAreAllClassified is the point of deriving the
// vocabulary rather than storing it: history written before it existed gets
// classified too.
//
// Set SHELLBEAM_CORPUS_STATE to a copy of a real state directory. The corpus
// this was built against held 671 non-success receipts, 667 of which carried no
// reason at all -- every one of them has to come out with a stage, a class and a
// code, or the vocabulary has a hole in it.
func TestRealCorpusFailuresAreAllClassified(t *testing.T) {
	root := os.Getenv("SHELLBEAM_CORPUS_STATE")
	if root == "" {
		t.Skip("SHELLBEAM_CORPUS_STATE not set")
	}
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}

	codes := map[string]int{}
	classes := map[receipt.FailureClass]int{}
	succeeded, failed, unclassified := 0, 0, 0
	for _, entry := range entries {
		var rec receipt.Receipt
		if err := readStrict(filepath.Join(root, "sessions", entry.Name(), "receipt.json"), &rec); err != nil {
			continue
		}
		failure := rec.Failure()
		if rec.State == session.Completed {
			succeeded++
			if failure != nil {
				t.Fatalf("%s completed but was classified as %#v", entry.Name(), failure)
			}
			continue
		}
		failed++
		if failure == nil {
			unclassified++
			t.Errorf("%s ended %q with no interpretation: %#v", entry.Name(), rec.State, rec.Exit)
			continue
		}
		if failure.Stage == "" || failure.Class == "" || failure.Code == "" {
			t.Fatalf("%s produced an incomplete interpretation %#v", entry.Name(), failure)
		}
		codes[failure.Code]++
		classes[failure.Class]++
	}

	if failed == 0 {
		t.Fatal("the corpus held no failures; this proves nothing")
	}
	if unclassified > 0 {
		t.Fatalf("%d of %d failures went uninterpreted", unclassified, failed)
	}
	t.Logf("%d succeeded, %d failed, all interpreted", succeeded, failed)
	for _, code := range sortedKeys(codes) {
		t.Logf("  %-24s %d", code, codes[code])
	}

	// The corpus is mostly commands reporting their own failure. If that is not
	// the dominant class, the vocabulary is mislabelling ordinary work as
	// something wrong with ShellBeam.
	if classes[receipt.ClassCommandFailed] == 0 {
		t.Fatal("no failure was attributed to the command itself")
	}
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	return keys
}
