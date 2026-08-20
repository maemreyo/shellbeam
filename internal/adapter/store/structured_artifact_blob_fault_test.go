package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryClaimWriteFaultLeavesNoRecoveryAuthority(t *testing.T) {
	r := openStructuredRepository(t)
	_, claim, request := artifactBlobFixture(t, r, "artifact-claim-fault", []byte("claim"))
	defer r.ReleaseBlobReservation(context.Background(), request.Reservation)
	r.writer.fail = failArtifactBlobPoint("artifact_recovery_claim.write")
	if err := r.PutRecoveryClaim(context.Background(), claim); err == nil {
		t.Fatal("recovery claim write fault returned nil error")
	}
	if _, err := os.Stat(r.artifactRecoveryPath(claim.CaptureAuthorityID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed recovery claim became visible: %v", err)
	}
}

func TestArtifactBlobFaultBoundariesDoNotMintPartialAuthority(t *testing.T) {
	for _, point := range []string{
		"artifact_blob.content_sync",
		"artifact_blob.metadata_write",
		"artifact_blob.metadata_sync",
		"artifact_blob.stage_sync",
		"artifact_blob.rename",
	} {
		t.Run(point, func(t *testing.T) {
			r := openStructuredRepository(t)
			_, claim, request := artifactBlobFixture(t, r, "artifact-fault-"+strings.ReplaceAll(point, ".", "-"), []byte("<testsuite/>"))
			if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
				t.Fatal(err)
			}
			r.writer.fail = failArtifactBlobPoint(point)
			if _, err := r.CommitArtifactBlob(context.Background(), request); err == nil {
				t.Fatalf("fault %s returned success", point)
			}
			if _, err := os.Stat(r.artifactBlobPath(request.BlobID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fault %s exposed final blob: %v", point, err)
			}
			assertNoArtifactStageDirs(t, r)
			if err := r.ReleaseBlobReservation(context.Background(), request.Reservation); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestArtifactBlobParentSyncAmbiguityResolvesOnlyFromExactPrivateDestination(t *testing.T) {
	r := openStructuredRepository(t)
	_, claim, request := artifactBlobFixture(t, r, "artifact-parent-sync", []byte("<testsuite/>"))
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	r.writer.fail = failArtifactBlobPoint("artifact_blob.parent_sync")
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatalf("exact private destination did not resolve ambiguity: %v", err)
	}
	resolved, err := r.ResolveArtifactBlob(context.Background(), request.BlobID)
	if err != nil || resolved != ref {
		t.Fatalf("resolved=%#v ref=%#v err=%v", resolved, ref, err)
	}
	assertNoArtifactStageDirs(t, r)
	if err := r.ReleaseBlobReservation(context.Background(), request.Reservation); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactBlobResolveFailsClosedOnPrivateContentCorruptionAndIgnoresStaging(t *testing.T) {
	r := openStructuredRepository(t)
	payload := []byte("abcdef")
	_, claim, request := artifactBlobFixture(t, r, "artifact-corrupt", payload)
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	contentPath := filepath.Join(r.artifactBlobPath(ref.BlobID), "content")
	if err := os.WriteFile(contentPath, []byte("ABCDEF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveArtifactBlob(context.Background(), ref.BlobID); err == nil {
		t.Fatal("corrupt private blob content retained authority")
	}

	stage := filepath.Join(r.artifactBlobRoot(), ".artifact-stage-orphan")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(r.artifactBlobPath(ref.BlobID), filepath.Join(stage, "not-authority")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveArtifactBlob(context.Background(), ref.BlobID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staging directory became authority: %v", err)
	}
}

func failArtifactBlobPoint(point string) func(string) error {
	failed := false
	return func(got string) error {
		if !failed && got == point {
			failed = true
			return errors.New("injected artifact blob fault: " + point)
		}
		return nil
	}
}

func assertNoArtifactStageDirs(t *testing.T, r *Repository) {
	t.Helper()
	entries, err := os.ReadDir(r.artifactBlobRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".artifact-stage-") {
			t.Fatalf("staging directory survived failure: %s", entry.Name())
		}
	}
}
