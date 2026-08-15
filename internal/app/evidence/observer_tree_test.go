package evidence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

func TestObserverTreeSHA256IsRootIndependentAndLexicallyDeterministic(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeTreeFixture(t, first, false)
	writeTreeFixture(t, second, true)
	output := []project.Output{{Path: "tree", Kind: "directory", Digest: "tree-sha256", Required: true}}
	one, err := NewObserver(DefaultLimits()).Observe(context.Background(), first, output)
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewObserver(DefaultLimits()).Observe(context.Background(), second, output)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Status != core.ArtifactCurrent || one[0].Digest == "" || one[0].Digest != two[0].Digest {
		t.Fatalf("one=%#v two=%#v", one, two)
	}
}

func TestObserverTreeEntryLimitNeverReturnsPartialDigest(t *testing.T) {
	root := t.TempDir()
	writeTreeFixture(t, root, false)
	limits := DefaultLimits()
	limits.MaxTreeEntries = 1
	got, err := NewObserver(limits).Observe(context.Background(), root, []project.Output{{Path: "tree", Kind: "directory", Digest: "tree-sha256", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactUnavailable || got[0].Quality != core.ObservationUnavailable || got[0].Digest != "" {
		t.Fatalf("limited tree=%#v", got)
	}
}

func writeTreeFixture(t *testing.T, root string, reverse bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "tree", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries := []struct{ path, data string }{{"tree/a.txt", "alpha"}, {"tree/sub/b.txt", "beta"}}
	if reverse {
		entries[0], entries[1] = entries[1], entries[0]
	}
	for _, entry := range entries {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(entry.path)), []byte(entry.data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObserverTreeMetadataBudgetIncludesSymlinkText(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tree"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Symlink("12345678901234567890", filepath.Join(root, "tree", name)); err != nil {
			t.Fatal(err)
		}
	}
	limits := DefaultLimits()
	limits.MaxMetadataBytes = 50
	got, err := NewObserver(limits).Observe(context.Background(), root, []project.Output{{Path: "tree", Kind: "directory", Digest: "tree-sha256", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactUnavailable || got[0].Digest != "" {
		t.Fatalf("metadata budget not enforced: %#v", got)
	}
}
