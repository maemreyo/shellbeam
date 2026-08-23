package structuredresult

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type materializerSource struct {
	data   []byte
	id     ArtifactSourceIdentity
	closed atomic.Int32
}

func (s *materializerSource) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}
func (s *materializerSource) StatIdentity() (ArtifactSourceIdentity, error) { return s.id, nil }
func (s *materializerSource) Close() error                                  { s.closed.Add(1); return nil }

type materializerBudgetCapability struct{ releases atomic.Int32 }

func (c *materializerBudgetCapability) Release() error { c.releases.Add(1); return nil }

type materializerRepoFake struct {
	record    CaptureAuthorityRecord
	order     []string
	reserved  BlobByteReservation
	released  []BlobByteReservation
	claim     ArtifactRecoveryClaim
	commit    ArtifactBlobCommitRequest
	commitErr error
}

func (f *materializerRepoFake) FindCaptureAuthority(context.Context, operation.ID) (CaptureAuthorityRecord, error) {
	return f.record, nil
}
func (f *materializerRepoFake) ReserveBlobBytes(_ context.Context, captureID string, bytes int64) (BlobByteReservation, error) {
	f.order = append(f.order, "reserve")
	f.reserved = BlobByteReservation{CaptureAuthorityID: captureID, Bytes: bytes}
	return f.reserved, nil
}
func (f *materializerRepoFake) ReleaseBlobReservation(_ context.Context, reservation BlobByteReservation) error {
	f.released = append(f.released, reservation)
	return nil
}
func (f *materializerRepoFake) PutRecoveryClaim(_ context.Context, claim ArtifactRecoveryClaim) error {
	f.order = append(f.order, "claim")
	f.claim = claim
	return nil
}
func (f *materializerRepoFake) GetRecoveryClaim(context.Context, string) (ArtifactRecoveryClaim, error) {
	return f.claim, nil
}
func (f *materializerRepoFake) CommitArtifactBlob(_ context.Context, req ArtifactBlobCommitRequest) (core.ArtifactBlobRef, error) {
	f.order = append(f.order, "commit")
	f.commit = req
	if f.commitErr != nil {
		return core.ArtifactBlobRef{}, f.commitErr
	}
	return core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: req.BlobID,
		OperationID: req.Intent.OperationID, SessionID: req.Intent.SessionID,
		RepositoryID: req.Intent.RepositoryID, WorkspaceID: req.Intent.WorkspaceID,
		DeclaredPath: req.Intent.DeclaredPathToken, NormalizedWorkspacePath: req.Intent.NormalizedWorkspacePath,
		SHA256: strings.Repeat("a", 64), Size: req.PreSourceIdentity.Size, TerminalCut: req.TerminalCut,
		ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("b", 64)},
	}, nil
}
func (f *materializerRepoFake) ResolveArtifactBlob(context.Context, string) (core.ArtifactBlobRef, error) {
	return core.ArtifactBlobRef{}, errors.New("unused")
}

func materializerAuthority(t *testing.T) CaptureAuthority {
	t.Helper()
	root := t.TempDir()
	binding := qualifiedPytestBinding(t, root, environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"})
	producerDigest, err := binding.ProducerBindingDigest()
	if err != nil {
		t.Fatal(err)
	}
	intent := ArtifactCaptureIntent{
		SchemaVersion: ArtifactCaptureIntentSchemaV1,
		OperationID:   "materialize-op", SessionID: "materialize-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		AdapterID: PytestJUnitAdapterID, DeclaredPathToken: binding.JUnitOutput.DeclaredPathToken,
		NormalizedWorkspacePath: binding.JUnitOutput.NormalizedWorkspacePath,
		ExpectedKind:            CaptureExpectedRegularFile, MaxBlobBytes: DefaultMaxArtifactBlobBytes,
		ProducerBindingDigest: producerDigest,
		Baseline:              CaptureBaselineIdentity{SchemaVersion: CaptureBaselineSchemaV1, State: CaptureBaselineAbsent, AuthorityDigest: strings.Repeat("c", 64)},
	}
	return CaptureAuthority{SchemaVersion: CaptureAuthoritySchemaV1, ProducerInvocationBinding: ProducerInvocationBinding{Kind: ProducerInvocationPytest, PytestInvocation: &binding}, Intent: intent}
}

func materializerReceipt(authority CaptureAuthority) receipt.Receipt {
	return receipt.Receipt{
		SchemaVersion: 1, OperationID: authority.Intent.OperationID, SessionID: authority.Intent.SessionID,
		Fingerprint: "fp", DaemonIncarnation: "daemon", State: session.Failed, Outcome: session.Failure,
		OutputComplete: true,
	}
}

func materializerTerminalResult(t *testing.T, authority CaptureAuthority, source *materializerSource, budget *materializerBudgetCapability) TerminalCaptureResult {
	t.Helper()
	digest, err := authority.StructuredCaptureDigest()
	if err != nil {
		t.Fatal(err)
	}
	return TerminalCaptureResult{
		State: TerminalCaptureAcquired, CaptureAuthorityID: digest, SourceIdentity: source.id,
		owner: &terminalCaptureOwnership{source: source, budget: budget},
	}
}

func TestMaterializerPersistsRecoveryClaimBeforeBlobCommitAndConsumesCaptureOnce(t *testing.T) {
	authority := materializerAuthority(t)
	record, err := NewCaptureAuthorityRecord(authority)
	if err != nil {
		t.Fatal(err)
	}
	source := &materializerSource{data: []byte("<testsuite/>")}
	source.id = ArtifactSourceIdentity{Scheme: ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("d", 64), Size: int64(len(source.data))}
	budget := &materializerBudgetCapability{}
	repo := &materializerRepoFake{record: record}

	ref, err := NewMaterializer(repo).Materialize(context.Background(), materializerTerminalResult(t, authority, source, budget), materializerReceipt(authority))
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("ref=%#v err=%v", ref, err)
	}
	if got := strings.Join(repo.order, ","); got != "reserve,claim,commit" {
		t.Fatalf("materialization order=%s", got)
	}
	if repo.reserved.Bytes != source.id.Size+ArtifactBlobMetadataOverhead || repo.reserved.CaptureAuthorityID != record.StructuredCaptureDigest {
		t.Fatalf("reservation=%#v", repo.reserved)
	}
	if repo.claim.BlobID != ref.BlobID || repo.claim.CaptureAuthorityID != record.StructuredCaptureDigest || repo.claim.TerminalCut != ref.TerminalCut {
		t.Fatalf("claim=%#v ref=%#v", repo.claim, ref)
	}
	if repo.commit.BlobID != ref.BlobID || repo.commit.Source != source || repo.commit.PreSourceIdentity != source.id || repo.commit.Reservation != repo.reserved {
		t.Fatalf("commit request=%#v", repo.commit)
	}
	if source.closed.Load() != 1 || budget.releases.Load() != 1 || len(repo.released) != 1 {
		t.Fatalf("cleanup source=%d budget=%d reservations=%d", source.closed.Load(), budget.releases.Load(), len(repo.released))
	}
}

func TestMaterializerFailureStillReleasesReservationAndCaptureOnce(t *testing.T) {
	authority := materializerAuthority(t)
	record, err := NewCaptureAuthorityRecord(authority)
	if err != nil {
		t.Fatal(err)
	}
	source := &materializerSource{data: []byte("broken")}
	source.id = ArtifactSourceIdentity{Scheme: ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("e", 64), Size: int64(len(source.data))}
	budget := &materializerBudgetCapability{}
	repo := &materializerRepoFake{record: record, commitErr: errors.New("commit failed")}

	if _, err := NewMaterializer(repo).Materialize(context.Background(), materializerTerminalResult(t, authority, source, budget), materializerReceipt(authority)); err == nil {
		t.Fatal("materialization failure returned nil error")
	}
	if source.closed.Load() != 1 || budget.releases.Load() != 1 || len(repo.released) != 1 {
		t.Fatalf("failure cleanup source=%d budget=%d reservations=%d", source.closed.Load(), budget.releases.Load(), len(repo.released))
	}
}

func TestMaterializerRejectsPhaseAIdentityDriftBeforeBlobReservation(t *testing.T) {
	authority := materializerAuthority(t)
	record, err := NewCaptureAuthorityRecord(authority)
	if err != nil {
		t.Fatal(err)
	}
	source := &materializerSource{data: []byte("data")}
	source.id = ArtifactSourceIdentity{Scheme: ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("f", 64), Size: 4}
	budget := &materializerBudgetCapability{}
	result := materializerTerminalResult(t, authority, source, budget)
	result.SourceIdentity.Digest = strings.Repeat("0", 64)
	repo := &materializerRepoFake{record: record}

	if _, err := NewMaterializer(repo).Materialize(context.Background(), result, materializerReceipt(authority)); !errors.Is(err, ErrArtifactChangedDuringCapture) {
		t.Fatalf("identity drift err=%v", err)
	}
	if repo.reserved.Bytes != 0 || len(repo.order) != 0 {
		t.Fatalf("identity drift reached persistence: reservation=%#v order=%v", repo.reserved, repo.order)
	}
	if source.closed.Load() != 1 || budget.releases.Load() != 1 {
		t.Fatalf("identity drift cleanup source=%d budget=%d", source.closed.Load(), budget.releases.Load())
	}
}

func TestArtifactBlobIDBindsExecutionPathButNotCapturePolicy(t *testing.T) {
	authority := materializerAuthority(t)
	base := authority.Intent
	first, err := ArtifactBlobID(base)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := ArtifactBlobID(base)
	if err != nil || replay != first {
		t.Fatalf("replay=%q first=%q err=%v", replay, first, err)
	}

	policyChanged := base
	policyChanged.Baseline.AuthorityDigest = strings.Repeat("9", 64)
	policyChanged.ProducerBindingDigest = strings.Repeat("8", 64)
	policyID, err := ArtifactBlobID(policyChanged)
	if err != nil || policyID != first {
		t.Fatalf("capture policy changed storage identity: first=%q changed=%q err=%v", first, policyID, err)
	}

	pathChanged := base
	pathChanged.NormalizedWorkspacePath = "reports/other.xml"
	pathChanged.DeclaredPathToken = "reports/other.xml"
	pathID, err := ArtifactBlobID(pathChanged)
	if err != nil || pathID == first {
		t.Fatalf("path did not change blob id: %q %q err=%v", first, pathID, err)
	}

	opChanged := base
	opChanged.OperationID = "materialize-other-op"
	opChanged.SessionID = "materialize-other-session"
	opID, err := ArtifactBlobID(opChanged)
	if err != nil || opID == first {
		t.Fatalf("operation did not change blob id: %q %q err=%v", first, opID, err)
	}
}

func TestArtifactObservationCutDigestBindsFinalSourceStability(t *testing.T) {
	base := ArtifactCaptureObservationCutV1{
		SchemaVersion:       ArtifactCaptureObservationCutSchemaV1,
		CaptureIntentDigest: strings.Repeat("1", 64), BaselineAuthorityDigest: strings.Repeat("2", 64),
		SourceObservationScheme:    ArtifactSourceIdentityUnixV1,
		PhaseASourceIdentityDigest: strings.Repeat("3", 64), PhaseASize: 7,
		FinalSourceIdentityDigest: strings.Repeat("3", 64), FinalSize: 7, StabilityResult: ArtifactSourceStabilityStable,
	}
	first, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.FinalSourceIdentityDigest = strings.Repeat("4", 64)
	second, err := changed.Digest()
	if err != nil || second == first {
		t.Fatalf("final source authority not bound: %q %q err=%v", first, second, err)
	}
}
