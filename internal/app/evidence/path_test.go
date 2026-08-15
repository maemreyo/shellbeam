package evidence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

func TestObserverSymlinkUsesLstatReadlinkAndDoesNotFollowTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("do-not-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "artifact-link")); err != nil {
		t.Fatal(err)
	}

	got, err := NewObserver(DefaultLimits()).Observe(context.Background(), root, []project.Output{{Path: "artifact-link", Kind: "symlink", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactCurrent || got[0].ObservedKind != "symlink" || got[0].LinkText != secret || got[0].Digest != "" {
		t.Fatalf("symlink=%#v", got)
	}
}

func TestObserverRejectsEscapingIntermediateSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	got, err := NewObserver(DefaultLimits()).Observe(context.Background(), root, []project.Output{{Path: "escape/secret.txt", Kind: "file", Digest: "sha256", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactUnavailable || got[0].Quality != core.ObservationUnavailable || got[0].Digest != "" {
		t.Fatalf("escape=%#v", got)
	}
}

func TestObserverBoundsSymlinkMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("12345678", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.MaxMetadataBytes = 4
	got, err := NewObserver(limits).Observe(context.Background(), root, []project.Output{{Path: "link", Kind: "symlink", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != core.ArtifactUnavailable || got[0].Quality != core.ObservationUnavailable || got[0].LinkText != "" {
		t.Fatalf("bounded link=%#v", got)
	}
}
