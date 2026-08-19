//go:build darwin || linux

package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"golang.org/x/sys/unix"
)

func TestArtifactBaselinePinsDescriptorRelativeAbsentParent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "reports", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	authority, baseline, err := QualifyArtifactAbsentBaseline(context.Background(), root, "reports/nested/junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if baseline.SchemaVersion != structuredapp.CaptureBaselineSchemaV1 || baseline.State != structuredapp.CaptureBaselineAbsent || len(baseline.AuthorityDigest) != 64 {
		t.Fatalf("baseline=%#v", baseline)
	}
	if authority.NormalizedWorkspacePath() != "reports/nested/junit.xml" || authority.FinalName() != "junit.xml" || authority.BaselineDigest() != baseline.AuthorityDigest {
		t.Fatalf("authority path=%q final=%q digest=%q baseline=%#v", authority.NormalizedWorkspacePath(), authority.FinalName(), authority.BaselineDigest(), baseline)
	}
	var st unix.Stat_t
	if err := unix.Fstat(authority.parentFD, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		t.Fatalf("pinned parent fd invalid: stat=%#v err=%v", st, err)
	}
}

func TestArtifactBaselineRejectsPreexistingFinalWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "junit.xml")
	if err := os.WriteFile(path, []byte("old-report"), 0o600); err != nil {
		t.Fatal(err)
	}
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if !errors.Is(err, ErrArtifactPreexisting) || authority != nil {
		t.Fatalf("authority=%#v err=%v", authority, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "old-report" {
		t.Fatalf("preexisting artifact changed: data=%q err=%v", got, readErr)
	}
}

func TestArtifactBaselineRejectsSymlinkedComponentAndOutsidePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"linked/junit.xml", "../outside.xml", "/tmp/outside.xml", "reports/../junit.xml", "reports//junit.xml"} {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, path)
			if !errors.Is(err, ErrArtifactPathUnqualified) || authority != nil {
				t.Fatalf("path=%q authority=%#v err=%v", path, authority, err)
			}
		})
	}
}

func TestArtifactBaselineRejectsFinalSymlinkAsPreexisting(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("missing-target", filepath.Join(root, "junit.xml")); err != nil {
		t.Fatal(err)
	}
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if !errors.Is(err, ErrArtifactPreexisting) || authority != nil {
		t.Fatalf("authority=%#v err=%v", authority, err)
	}
}

func TestArtifactBaselineRejectsParentSwapDuringQualification(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "reports")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := artifactBaselineResolver{hooks: &artifactCaptureHooks{checkpoint: func(stage string) {
		if stage != "after-absent-check" {
			return
		}
		if err := os.Rename(parent, filepath.Join(root, "reports-old")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
	}}}
	authority, _, err := resolver.qualifyAbsent(context.Background(), root, "reports/junit.xml")
	if !errors.Is(err, ErrArtifactPathUnqualified) || authority != nil {
		t.Fatalf("parent swap authority=%#v err=%v", authority, err)
	}
}

func TestArtifactPathAuthorityCloseReleasesPinnedParent(t *testing.T) {
	root := t.TempDir()
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	fd := authority.parentFD
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
	if err := authority.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); !errors.Is(err, unix.EBADF) {
		t.Fatalf("parent fd still live after close: %v", err)
	}
}
