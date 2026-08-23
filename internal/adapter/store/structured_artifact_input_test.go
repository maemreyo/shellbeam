package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestArtifactBlobRangeReadsOnlyExactRetainedPrivateRef(t *testing.T) {
	r := openStructuredRepository(t)
	payload := []byte("0123456789")
	_, claim, request := artifactBlobFixture(t, r, "artifact-input-range", payload)
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadArtifactBlobRange(context.Background(), ref, 3, 4)
	if err != nil || string(got) != "3456" {
		t.Fatalf("range=%q err=%v", got, err)
	}
	got, err = r.ReadArtifactBlobRange(context.Background(), ref, ref.Size, 4)
	if err != nil || len(got) != 0 {
		t.Fatalf("end range=%q err=%v", got, err)
	}
	changed := ref
	changed.TerminalCut.ReceiptDigest = strings.Repeat("f", 64)
	if _, err := r.ReadArtifactBlobRange(context.Background(), changed, 0, 1); !errors.Is(err, structuredapp.ErrArtifactInputUnavailable) {
		t.Fatalf("mismatched ref err=%v", err)
	}
	if _, err := r.ReadArtifactBlobRange(context.Background(), ref, -1, 1); err == nil {
		t.Fatal("negative range accepted")
	}
}

func TestArtifactBlobRangeReturnsTypedCompactedAndCorruptUnavailable(t *testing.T) {
	t.Run("compacted", func(t *testing.T) {
		r := openStructuredRepository(t)
		blob, _ := seedRetainedArtifactBlob(t, r, "artifact-input-compacted", []byte("abcdef"))
		pending := artifactPendingDerivation(t, blob, 'c')
		if err := r.PutDerivation(context.Background(), pending); err != nil {
			t.Fatal(err)
		}
		terminalizeArtifactDerivation(t, r, pending)
		if err := r.CompactDerivationDetail(context.Background(), pending.DerivationKey); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ReadArtifactBlobRange(context.Background(), blob, 0, 1); !errors.Is(err, structuredapp.ErrArtifactInputCompacted) {
			t.Fatalf("compacted read err=%v", err)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		r := openStructuredRepository(t)
		blob, _ := seedRetainedArtifactBlob(t, r, "artifact-input-corrupt", []byte("abcdef"))
		content := filepath.Join(r.artifactBlobPath(blob.BlobID), "content")
		if err := os.WriteFile(content, []byte("abcdeg"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ReadArtifactBlobRange(context.Background(), blob, 0, 1); !errors.Is(err, structuredapp.ErrArtifactInputUnavailable) {
			t.Fatalf("corrupt read err=%v", err)
		}
	})
}

func TestArtifactRecoveryCandidatesUseDurableClaimBlobAndCaptureAuthority(t *testing.T) {
	r := openStructuredRepository(t)
	authority, claim, request := artifactBlobFixture(t, r, "artifact-worker-recovery", []byte("{}"))
	record, created, err := r.ReserveCaptureAuthority(context.Background(), authority)
	if err != nil || !created {
		t.Fatalf("capture authority created=%v err=%v", created, err)
	}
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := r.ListArtifactRecoveryCandidates(context.Background(), 8)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if !reflect.DeepEqual(candidates[0], structuredapp.ArtifactRecoveryCandidate{Ref: ref, CaptureAuthority: record}) {
		t.Fatalf("candidate=%#v", candidates[0])
	}

	producer := core.Producer{AdapterID: authority.Intent.AdapterID, AdapterVersion: 1, CapabilityVersion: 1}
	key, err := core.DerivationKeyForInputs([]core.StructuredInputRef{core.ArtifactInputRef(ref)}, producer, 1, strings.Repeat("9", 64))
	if err != nil {
		t.Fatal(err)
	}
	d := core.Derivation{SchemaVersion: core.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: []core.StructuredInputRef{core.ArtifactInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("9", 64), Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable}
	if err := r.PutDerivation(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	candidates, err = r.ListArtifactRecoveryCandidates(context.Background(), 8)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("bound claim remained recoverable: %#v err=%v", candidates, err)
	}
}
