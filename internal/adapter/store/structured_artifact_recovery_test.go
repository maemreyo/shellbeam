package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestSessionRetentionCannotDestroyCommittedUnboundArtifactRecoveryAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return old }
	res := operation.Reservation{
		SchemaVersion: 1, OperationID: operation.ID("retained-artifact-op"), SessionID: operation.SessionID("retained-artifact-op-session"),
		Fingerprint: "fingerprint", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon",
	}
	reserveOK(t, r, res)
	terminateOK(t, r, res)
	snapshot, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.UpdatedAt = old
	if result := r.AdvanceSession(context.Background(), snapshot); result.Err != nil {
		t.Fatal(result.Err)
	}
	blob, claim := seedRetainedArtifactBlob(t, r, string(res.OperationID), []byte("<testsuite/>"))

	report, err := r.CollectExpiredTerminals(context.Background(), RetentionPolicy{TerminalRetention: time.Hour, Now: func() time.Time { return old.Add(2 * time.Hour) }})
	if err != nil || report.Collected != 1 {
		t.Fatalf("retention report=%#v err=%v", report, err)
	}
	if _, err := os.Stat(r.operationPath(res.OperationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("operation bulk history survived retention: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", string(res.SessionID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session bulk history survived retention: %v", err)
	}
	if got, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); err != nil || !reflect.DeepEqual(got, claim) {
		t.Fatalf("retention destroyed recovery claim: got=%#v err=%v", got, err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained {
		t.Fatalf("retention destroyed blob authority: state=%#v err=%v", state, err)
	}

	reopened := openStructuredRepositoryAt(t, root)
	if got, err := reopened.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); err != nil || !reflect.DeepEqual(got, claim) {
		t.Fatalf("reopen lost retained recovery claim: got=%#v err=%v", got, err)
	}
	pending := artifactPendingDerivation(t, blob, '9')
	if err := reopened.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reopened.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("recovered claim did not hand off to durable detail ref: %v", err)
	}
	if _, err := reopened.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bound claim remained recovery-live: %v", err)
	}
}

func TestRecoverStructuredArtifactsRebuildsMissingDetailedRefBeforeReleasingClaim(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-recover-ref", []byte("<testsuite/>"))
	pending := artifactPendingDerivation(t, blob, 'a')
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactBlobRefRoot()); err != nil {
		t.Fatal(err)
	}
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}

	if err := r.RecoverStructuredArtifacts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("recovery did not restore detailed ref: %v", err)
	}
	if _, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim released before/without recovered detail authority: %v", err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained || state.Ref.TerminalCut != blob.TerminalCut || state.Ref.ObservationCut != blob.ObservationCut {
		t.Fatalf("recovery regenerated or changed cuts: state=%#v err=%v", state, err)
	}
}

func TestRecoverStructuredArtifactsFinishesRetirementBarrierToTombstone(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-retire-recovery", []byte("<testsuite/>"))
	if err := os.Remove(r.artifactRecoveryPath(claim.CaptureAuthorityID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactRecoveryRoot()); err != nil {
		t.Fatal(err)
	}
	staged := r.artifactRetirementPath(blob.BlobID)
	if err := os.Rename(r.artifactBlobPath(blob.BlobID), staged); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
		t.Fatal(err)
	}

	if err := r.RecoverStructuredArtifacts(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobCompacted || state.Tombstone.BlobID != blob.BlobID {
		t.Fatalf("retirement recovery state=%#v err=%v", state, err)
	}
	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retirement staging survived recovery: %v", err)
	}
}

func TestCollectArtifactOrphansDeletesNeverAuthoritativeStagesButPreservesLiveClaims(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-orphan-live", []byte("<testsuite/>"))
	stage := filepath.Join(r.artifactBlobRoot(), ".artifact-stage-never-authority")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.CollectArtifactOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging orphan survived collection: %v", err)
	}
	if got, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); err != nil || got.BlobID != blob.BlobID {
		t.Fatalf("orphan collector destroyed live claim: got=%#v err=%v", got, err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained {
		t.Fatalf("orphan collector retired live-claimed blob: state=%#v err=%v", state, err)
	}
}

func TestOpenRunsStructuredRecoveryBeforeServingStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-open-recovery", []byte("<testsuite/>"))
	if err := os.Remove(r.artifactRecoveryPath(claim.CaptureAuthorityID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactRecoveryRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(r.artifactBlobPath(blob.BlobID), r.artifactRetirementPath(blob.BlobID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
		t.Fatal(err)
	}

	reopened := openStructuredRepositoryAt(t, root)
	state, err := reopened.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobCompacted {
		t.Fatalf("Open did not reconcile retirement before serving state=%#v err=%v", state, err)
	}
}

var _ = core.ArtifactBlobRef{}

func TestCollectArtifactOrphansRetiresCommittedBlobWithNoClaimOrDetailRef(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-orphan-retire", []byte("<testsuite/>"))
	if err := os.Remove(r.artifactRecoveryPath(claim.CaptureAuthorityID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactRecoveryRoot()); err != nil {
		t.Fatal(err)
	}
	if err := r.CollectArtifactOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobCompacted || state.Tombstone.BlobID != blob.BlobID {
		t.Fatalf("orphan retirement state=%#v err=%v", state, err)
	}
	if _, err := os.Stat(r.artifactBlobPath(blob.BlobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan retirement left content authority: %v", err)
	}
}

func TestAbandonUnresolvedRunsStructuredRecoveryBeforeSessionReconcile(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-abandon-recovery", []byte("<testsuite/>"))
	if err := os.Remove(r.artifactRecoveryPath(claim.CaptureAuthorityID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactRecoveryRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(r.artifactBlobPath(blob.BlobID), r.artifactRetirementPath(blob.BlobID)); err != nil {
		t.Fatal(err)
	}
	if err := syncPrivateDirectory(r.artifactBlobRoot()); err != nil {
		t.Fatal(err)
	}
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobCompacted {
		t.Fatalf("startup reconcile did not finish structured retirement: state=%#v err=%v", state, err)
	}
}
