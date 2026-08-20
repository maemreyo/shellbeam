//go:build darwin || linux

package localfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestArtifactSourceHandlePinsFinalObjectAndIgnoresPathReplacement(t *testing.T) {
	root := t.TempDir()
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("original-junit")
	if err := os.WriteFile(filepath.Join(root, "junit.xml"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	source, identity, err := authority.OpenArtifactSource(context.Background(), strings.Repeat("a", 64), structuredapp.DefaultMaxArtifactBlobBytes)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Validate() != nil || identity.Size != int64(len(original)) || identity.Scheme != structuredapp.ArtifactSourceIdentityUnixV1 {
		t.Fatalf("identity=%#v", identity)
	}
	handle, ok := source.(*artifactSourceHandle)
	if !ok || handle.captureAuthorityID != strings.Repeat("a", 64) || handle.parentFD < 0 || handle.fileFD < 0 {
		t.Fatalf("handle=%#v", source)
	}
	if authority.parentFD != -1 || !authority.closed {
		t.Fatalf("path authority did not transfer parent fd: parent=%d closed=%v", authority.parentFD, authority.closed)
	}
	if err := os.Rename(filepath.Join(root, "junit.xml"), filepath.Join(root, "junit-old.xml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "junit.xml"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(source)
	if err != nil || string(got) != string(original) {
		t.Fatalf("pinned bytes=%q err=%v", got, err)
	}
	post, err := source.StatIdentity()
	if err != nil || post.Validate() != nil || post.Size != identity.Size {
		t.Fatalf("post identity=%#v initial=%#v err=%v", post, identity, err)
	}
	parentFD, fileFD := handle.parentFD, handle.fileFD
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(parentFD, &st); !errors.Is(err, unix.EBADF) {
		t.Fatalf("parent fd remained live: %v", err)
	}
	if err := unix.Fstat(fileFD, &st); !errors.Is(err, unix.EBADF) {
		t.Fatalf("file fd remained live: %v", err)
	}
}

func TestArtifactSourceOpenFailsClosedForMissingKindAndBudget(t *testing.T) {
	cases := []struct {
		name string
		make func(string) error
		max  int64
		want error
	}{
		{"missing", func(string) error { return nil }, structuredapp.DefaultMaxArtifactBlobBytes, structuredapp.ErrArtifactSourceMissing},
		{"directory", func(path string) error { return os.Mkdir(path, 0o700) }, structuredapp.DefaultMaxArtifactBlobBytes, structuredapp.ErrArtifactSourceKindMismatch},
		{"symlink", func(path string) error { return os.Symlink("target", path) }, structuredapp.DefaultMaxArtifactBlobBytes, structuredapp.ErrArtifactSourceKindMismatch},
		{"budget", func(path string) error { return os.WriteFile(path, []byte("12345"), 0o600) }, 4, structuredapp.ErrArtifactSourceBudgetExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.make(filepath.Join(root, "junit.xml")); err != nil {
				t.Fatal(err)
			}
			source, _, err := authority.OpenArtifactSource(context.Background(), strings.Repeat("b", 64), tc.max)
			if !errors.Is(err, tc.want) || source != nil {
				t.Fatalf("source=%#v err=%v want=%v", source, err, tc.want)
			}
			if authority.parentFD != -1 || !authority.closed {
				t.Fatalf("failed terminal open retained path authority parent=%d closed=%v", authority.parentFD, authority.closed)
			}
		})
	}
}

func TestArtifactSourceIdentityChangesWhenPinnedObjectMutatesSameSize(t *testing.T) {
	root := t.TempDir()
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "junit.xml")
	if err := os.WriteFile(path, []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, before, err := authority.OpenArtifactSource(context.Background(), strings.Repeat("c", 64), structuredapp.DefaultMaxArtifactBlobBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	// Mutate the same inode without changing its size. Source identity must include
	// metadata beyond dev/inode/size so Task 6 can reject this during stability proof.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("bbbb"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	after, err := source.StatIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("same-size mutation did not change source identity: %#v", before)
	}
}

func TestArtifactSourceOpenHookRunsAfterFinalFDIsPinned(t *testing.T) {
	root := t.TempDir()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	resolver := artifactBaselineResolver{hooks: &artifactCaptureHooks{checkpoint: func(stage string) {
		if stage == "after-final-open" {
			entered <- struct{}{}
			<-release
		}
	}}}
	authority, _, err := resolver.qualifyAbsent(context.Background(), root, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "junit.xml"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		source, _, err := authority.OpenArtifactSource(context.Background(), strings.Repeat("d", 64), structuredapp.DefaultMaxArtifactBlobBytes)
		if source != nil {
			_ = source.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("final-open hook not reached")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
