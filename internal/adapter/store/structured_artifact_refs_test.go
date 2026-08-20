package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func seedRetainedArtifactBlob(t *testing.T, r *Repository, operationID string, payload []byte) (core.ArtifactBlobRef, structuredapp.ArtifactRecoveryClaim) {
	t.Helper()
	_, claim, request := artifactBlobFixture(t, r, operationID, payload)
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return ref, claim
}

func artifactPendingDerivation(t *testing.T, ref core.ArtifactBlobRef, configByte byte) core.Derivation {
	t.Helper()
	producer := core.Producer{AdapterID: structuredapp.PytestJUnitAdapterID, AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat(string(configByte), 64)
	input := core.ArtifactInputRef(ref)
	key, err := core.DerivationKeyForInputs([]core.StructuredInputRef{input}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	return core.Derivation{
		SchemaVersion: core.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{input},
		Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable,
	}
}

func terminalizeArtifactDerivation(t *testing.T, r *Repository, pending core.Derivation) core.Derivation {
	t.Helper()
	processing := pending
	processing.Lifecycle = core.LifecycleProcessing
	if err := r.PutDerivation(context.Background(), processing); err != nil {
		t.Fatal(err)
	}
	record := structuredArtifactRecord(processing, core.AuthorityMechanical, "artifact-detail")
	if err := r.PutRecords(context.Background(), pending.DerivationKey, []core.Record{record}); err != nil {
		t.Fatal(err)
	}
	terminal := processing
	terminal.Lifecycle = core.LifecycleTerminal
	terminal.ParseOutcome = core.ParseComplete
	terminal.Completeness = core.CompletenessComplete
	if err := r.PutDerivation(context.Background(), terminal); err != nil {
		t.Fatal(err)
	}
	return terminal
}

func TestArtifactPendingDerivationAcquiresDurableRefsBeforeVisibilityAndBindsRecoveryClaim(t *testing.T) {
	root := t.TempDir() + "/state"
	r := openStructuredRepositoryAt(t, root)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-ref-bind", []byte("<testsuite/>"))
	pending := artifactPendingDerivation(t, blob, '1')
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("durable blob ref missing after derivation visibility: %v", err)
	}
	if _, err := os.Stat(r.artifactRecoveryPath(claim.CaptureAuthorityID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bound recovery claim remained live: %v", err)
	}
	reopened := openStructuredRepositoryAt(t, root)
	state, err := reopened.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained || state.Ref != blob {
		t.Fatalf("reopened blob state=%#v err=%v", state, err)
	}
	if _, err := os.Stat(reopened.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("durable ref did not survive reopen: %v", err)
	}
}

func TestArtifactDerivationCreateFaultRollsBackRefsAndKeepsRecoveryClaim(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-ref-rollback", []byte("<testsuite/>"))
	pending := artifactPendingDerivation(t, blob, '2')
	failed := false
	r.writer.fail = func(point string) error {
		if point == "structured_derivation.create" && !failed {
			failed = true
			return errors.New("injected derivation create fault")
		}
		return nil
	}
	if err := r.PutDerivation(context.Background(), pending); err == nil {
		t.Fatal("derivation create fault returned success")
	}
	if _, err := os.Stat(r.derivationPath(pending.DerivationKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed derivation became visible: %v", err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed derivation retained blob ref: %v", err)
	}
	if _, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); err != nil {
		t.Fatalf("failed derivation destroyed recovery claim: %v", err)
	}
}

func TestArtifactRefAcquireAndRetirementBarrierSerialize(t *testing.T) {
	t.Run("retirement wins", func(t *testing.T) {
		r := openStructuredRepository(t)
		blob, _ := seedRetainedArtifactBlob(t, r, "artifact-retire-wins", []byte("<testsuite/>"))
		first := artifactPendingDerivation(t, blob, '3')
		if err := r.PutDerivation(context.Background(), first); err != nil {
			t.Fatal(err)
		}
		terminalizeArtifactDerivation(t, r, first)
		second := artifactPendingDerivation(t, blob, '4')

		r.structuredMu.Lock()
		started := make(chan struct{})
		putErr := make(chan error, 1)
		go func() {
			close(started)
			putErr <- r.PutDerivation(context.Background(), second)
		}()
		<-started
		if err := r.compactDerivationDetailUnlocked(context.Background(), first.DerivationKey); err != nil {
			r.structuredMu.Unlock()
			t.Fatal(err)
		}
		r.structuredMu.Unlock()
		if err := <-putErr; err == nil {
			t.Fatal("new detailed derivation attached after retirement barrier won")
		}
		state, err := r.ResolveArtifactBlobState(context.Background(), blob)
		if err != nil || state.State != ArtifactBlobCompacted {
			t.Fatalf("blob state=%#v err=%v", state, err)
		}
		if _, err := os.Stat(r.derivationPath(second.DerivationKey)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("losing derivation became visible: %v", err)
		}
	})

	t.Run("reference wins", func(t *testing.T) {
		r := openStructuredRepository(t)
		blob, _ := seedRetainedArtifactBlob(t, r, "artifact-ref-wins", []byte("<testsuite/>"))
		first := artifactPendingDerivation(t, blob, '5')
		if err := r.PutDerivation(context.Background(), first); err != nil {
			t.Fatal(err)
		}
		terminalizeArtifactDerivation(t, r, first)
		second := artifactPendingDerivation(t, blob, '6')

		r.structuredMu.Lock()
		started := make(chan struct{})
		compactErr := make(chan error, 1)
		go func() {
			close(started)
			compactErr <- r.CompactDerivationDetail(context.Background(), first.DerivationKey)
		}()
		<-started
		if err := r.putDerivationUnlocked(context.Background(), second); err != nil {
			r.structuredMu.Unlock()
			t.Fatal(err)
		}
		r.structuredMu.Unlock()
		if err := <-compactErr; err != nil {
			t.Fatal(err)
		}
		state, err := r.ResolveArtifactBlobState(context.Background(), blob)
		if err != nil || state.State != ArtifactBlobRetained {
			t.Fatalf("blob retired despite winning detailed ref: state=%#v err=%v", state, err)
		}
		if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, second.DerivationKey)); err != nil {
			t.Fatalf("winning derivation ref missing: %v", err)
		}
	})
}

func TestCompactionReleasesOnlyOwnBlobRefAndLastDetailRetiresBytes(t *testing.T) {
	r := openStructuredRepository(t)
	blob, _ := seedRetainedArtifactBlob(t, r, "artifact-multi-ref", []byte("<testsuite/>"))
	first := artifactPendingDerivation(t, blob, '7')
	second := artifactPendingDerivation(t, blob, '8')
	if err := r.PutDerivation(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := r.PutDerivation(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	terminalizeArtifactDerivation(t, r, first)
	terminalizeArtifactDerivation(t, r, second)

	if err := r.CompactDerivationDetail(context.Background(), first.DerivationKey); err != nil {
		t.Fatal(err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained {
		t.Fatalf("first compaction retired shared blob: state=%#v err=%v", state, err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, first.DerivationKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first ref survived compaction: %v", err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, second.DerivationKey)); err != nil {
		t.Fatalf("second ref lost during first compaction: %v", err)
	}

	if err := r.CompactDerivationDetail(context.Background(), second.DerivationKey); err != nil {
		t.Fatal(err)
	}
	state, err = r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobCompacted || state.Tombstone.BlobID != blob.BlobID || state.Tombstone.SHA256 != blob.SHA256 || state.Tombstone.Size != blob.Size {
		t.Fatalf("last compaction state=%#v err=%v", state, err)
	}
	if _, err := os.Stat(r.artifactBlobPath(blob.BlobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("compacted blob bytes remained visible: %v", err)
	}
}

func TestArtifactDerivationAmbiguousCreateKeepsRefAndClaimUntilExactReplay(t *testing.T) {
	r := openStructuredRepository(t)
	blob, claim := seedRetainedArtifactBlob(t, r, "artifact-ref-ambiguous", []byte("<testsuite/>"))
	pending := artifactPendingDerivation(t, blob, 'b')
	dirSyncs := 0
	r.writer.fail = func(point string) error {
		if point == "create.dir_sync" {
			dirSyncs++
			if dirSyncs == 2 {
				return errors.New("injected ambiguous derivation create")
			}
		}
		return nil
	}
	if err := r.PutDerivation(context.Background(), pending); err == nil {
		t.Fatal("ambiguous derivation create returned success")
	}
	if _, err := os.Stat(r.derivationPath(pending.DerivationKey)); err != nil {
		t.Fatalf("ambiguous create did not leave observable exact derivation: %v", err)
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("ambiguous create rolled back required ref: %v", err)
	}
	if _, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); err != nil {
		t.Fatalf("ambiguous create prematurely released recovery claim: %v", err)
	}

	r.writer.fail = nil
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatalf("exact replay after ambiguity: %v", err)
	}
	if _, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exact replay did not complete claim handoff: %v", err)
	}
}

func TestArtifactCompactionCannotBypassDetailReferenceProtocol(t *testing.T) {
	r := openStructuredRepository(t)
	blob, _ := seedRetainedArtifactBlob(t, r, "artifact-compaction-protocol", []byte("<testsuite/>"))
	pending := artifactPendingDerivation(t, blob, 'c')
	if err := r.PutDerivation(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	terminal := terminalizeArtifactDerivation(t, r, pending)
	bypass := terminal
	bypass.Completeness = core.CompletenessCompacted
	if err := r.PutDerivation(context.Background(), bypass); err == nil {
		t.Fatal("artifact derivation compacted without detail/ref retirement protocol")
	}
	if _, err := os.Stat(r.derivationBlobRefPath(blob.BlobID, pending.DerivationKey)); err != nil {
		t.Fatalf("rejected bypass changed blob ref: %v", err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err != nil || state.State != ArtifactBlobRetained {
		t.Fatalf("rejected bypass changed blob state=%#v err=%v", state, err)
	}
	if err := r.CompactDerivationDetail(context.Background(), pending.DerivationKey); err != nil {
		t.Fatal(err)
	}
}

func TestResolveArtifactBlobStateFailsClosedOnRetainedTombstoneConflict(t *testing.T) {
	r := openStructuredRepository(t)
	blob, _ := seedRetainedArtifactBlob(t, r, "artifact-state-conflict", []byte("<testsuite/>"))
	tombstone := ArtifactBlobTombstone{SchemaVersion: artifactBlobTombstoneSchemaV1, BlobID: blob.BlobID, SHA256: blob.SHA256, Size: blob.Size, State: ArtifactBlobCompacted}
	if result := r.writer.Create(r.artifactBlobTombstonePath(blob.BlobID), tombstone); result.Err != nil {
		t.Fatal(result.Err)
	}
	state, err := r.ResolveArtifactBlobState(context.Background(), blob)
	if err == nil || state.State != ArtifactBlobUnavailable {
		t.Fatalf("retained+tombstone conflict accepted: state=%#v err=%v", state, err)
	}
}
