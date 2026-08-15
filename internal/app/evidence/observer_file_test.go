package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

func TestObserverFileCurrentMissingKindAndSHA256(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist", "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("artifact payload\n")
	if err := os.WriteFile(filepath.Join(root, "dist", "report.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	observer := NewObserver(DefaultLimits())
	got, err := observer.Observe(context.Background(), root, []project.Output{
		{Path: "dist/report.txt", Kind: "file", Digest: "sha256", Required: true},
		{Path: "dist/missing.txt", Kind: "file", Required: true},
		{Path: "dist/optional.txt", Kind: "file", Required: false},
		{Path: "dist/dir", Kind: "file", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("observations=%#v", got)
	}
	sum := sha256.Sum256(content)
	if got[0].Status != core.ArtifactCurrent || got[0].Quality != core.ObservationComplete || got[0].ObservedKind != "file" || got[0].Digest != hex.EncodeToString(sum[:]) || got[0].Size != int64(len(content)) || got[0].ObservedAt.IsZero() {
		t.Fatalf("file=%#v", got[0])
	}
	if got[1].Status != core.ArtifactMissing || got[1].Exists || !got[1].Required || got[1].Quality != core.ObservationComplete {
		t.Fatalf("required missing=%#v", got[1])
	}
	if got[2].Status != core.ArtifactMissing || got[2].Required {
		t.Fatalf("optional missing=%#v", got[2])
	}
	if got[3].Status != core.ArtifactKindMismatch || got[3].ObservedKind != "directory" || !got[3].Exists {
		t.Fatalf("kind=%#v", got[3])
	}
}

func TestObserverFileDigestLimitIsUnavailableWithoutPartialIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.bin"), []byte("12345678"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.MaxDigestBytes = 4
	got, err := NewObserver(limits).Observe(context.Background(), root, []project.Output{{Path: "large.bin", Kind: "file", Digest: "sha256", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactUnavailable || got[0].Quality != core.ObservationUnavailable || got[0].Digest != "" {
		t.Fatalf("limited=%#v", got)
	}
}
