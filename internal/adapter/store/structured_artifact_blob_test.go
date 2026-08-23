package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type artifactBlobSource struct {
	mu         sync.Mutex
	data       []byte
	identities []structuredapp.ArtifactSourceIdentity
	statCalls  int
}

func (s *artifactBlobSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}
func (s *artifactBlobSource) StatIdentity() (structuredapp.ArtifactSourceIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.identities) == 0 {
		return structuredapp.ArtifactSourceIdentity{}, errors.New("missing source identity")
	}
	i := s.statCalls
	if i >= len(s.identities) {
		i = len(s.identities) - 1
	}
	s.statCalls++
	return s.identities[i], nil
}
func (s *artifactBlobSource) Close() error { return nil }

func artifactBlobReceipt(authority structuredapp.CaptureAuthority) receipt.Receipt {
	return receipt.Receipt{
		SchemaVersion: 1, OperationID: authority.Intent.OperationID, SessionID: authority.Intent.SessionID,
		Fingerprint: "artifact-fp", DaemonIncarnation: "daemon", State: session.Failed, Outcome: session.Failure, OutputComplete: true,
	}
}

func artifactBlobFixture(t *testing.T, r *Repository, operationID string, payload []byte) (structuredapp.CaptureAuthority, structuredapp.ArtifactRecoveryClaim, structuredapp.ArtifactBlobCommitRequest) {
	t.Helper()
	authority := testCaptureAuthority(t, operationID, "reports/junit.xml", 'a')
	captureID, err := authority.StructuredCaptureDigest()
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := structuredapp.ArtifactBlobID(authority.Intent)
	if err != nil {
		t.Fatal(err)
	}
	terminalCut, err := structuredapp.TerminalCutForReceipt(artifactBlobReceipt(authority))
	if err != nil {
		t.Fatal(err)
	}
	identity := structuredapp.ArtifactSourceIdentity{Scheme: structuredapp.ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("d", 64), Size: int64(len(payload))}
	reservation, err := r.ReserveBlobBytes(context.Background(), captureID, identity.Size+structuredapp.ArtifactBlobMetadataOverhead)
	if err != nil {
		t.Fatal(err)
	}
	claim := structuredapp.ArtifactRecoveryClaim{
		SchemaVersion: structuredapp.ArtifactRecoveryClaimSchemaV1, CaptureAuthorityID: captureID, BlobID: blobID,
		OperationID: authority.Intent.OperationID, SessionID: authority.Intent.SessionID,
		RepositoryID: authority.Intent.RepositoryID, WorkspaceID: authority.Intent.WorkspaceID,
		AdapterID: authority.Intent.AdapterID, TerminalCut: terminalCut,
	}
	source := &artifactBlobSource{data: append([]byte(nil), payload...), identities: []structuredapp.ArtifactSourceIdentity{identity, identity}}
	request := structuredapp.ArtifactBlobCommitRequest{
		CaptureAuthorityID: captureID, Intent: authority.Intent, BlobID: blobID, TerminalCut: terminalCut,
		PreSourceIdentity: identity, Source: source, Reservation: reservation,
	}
	return authority, claim, request
}

func TestArtifactBlobPrivateLayoutRecoveryClaimAndResolve(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	payload := []byte("<testsuite tests=\"1\"/>")
	_, claim, request := artifactBlobFixture(t, r, "artifact-blob-layout", payload)
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	ref, err := r.CommitArtifactBlob(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if reserved := r.outstandingBlobReservationBytes(); reserved != 0 {
		t.Fatalf("durable commit retained outstanding reservation bytes: %d", reserved)
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("ref=%#v err=%v", ref, err)
	}
	if ref.BlobID != claim.BlobID || ref.Size != int64(len(payload)) {
		t.Fatalf("ref=%#v", ref)
	}

	blobDir := r.artifactBlobPath(ref.BlobID)
	for path, perm := range map[string]os.FileMode{
		r.artifactBlobRoot(): 0o700, r.artifactRecoveryRoot(): 0o700, blobDir: 0o700,
		filepath.Join(blobDir, "metadata.json"): 0o600, filepath.Join(blobDir, "content"): 0o600,
		r.artifactRecoveryPath(claim.CaptureAuthorityID): 0o600,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != perm {
			t.Fatalf("path=%s mode=%v err=%v", path, infoMode(info), statErr)
		}
	}
	content, err := os.ReadFile(filepath.Join(blobDir, "content"))
	if err != nil || string(content) != string(payload) {
		t.Fatalf("content=%q err=%v", content, err)
	}

	resolved, err := r.ResolveArtifactBlob(context.Background(), ref.BlobID)
	if err != nil || !reflect.DeepEqual(resolved, ref) {
		t.Fatalf("resolved=%#v ref=%#v err=%v", resolved, ref, err)
	}
	gotClaim, err := r.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID)
	if err != nil || !reflect.DeepEqual(gotClaim, claim) {
		t.Fatalf("claim=%#v want=%#v err=%v", gotClaim, claim, err)
	}
	entries, err := os.ReadDir(r.artifactBlobRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".artifact-stage-") {
			t.Fatalf("staging authority leaked: %s", entry.Name())
		}
	}
	if err := r.ReleaseBlobReservation(context.Background(), request.Reservation); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryClaimIsCreateOnlyReplaySafeAndSurvivesReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	_, claim, request := artifactBlobFixture(t, r, "artifact-recovery", []byte("x"))
	defer r.ReleaseBlobReservation(context.Background(), request.Reservation)
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(r.artifactRecoveryPath(claim.CaptureAuthorityID))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatalf("replay: %v", err)
	}
	after, err := os.ReadFile(r.artifactRecoveryPath(claim.CaptureAuthorityID))
	if err != nil || string(before) != string(after) {
		t.Fatalf("replay rewrote claim err=%v", err)
	}
	changed := claim
	changed.BlobID = "abl_" + strings.Repeat("e", 64)
	if err := r.PutRecoveryClaim(context.Background(), changed); err == nil {
		t.Fatal("conflicting recovery claim accepted")
	}
	reopened := openStructuredRepositoryAt(t, root)
	got, err := reopened.GetRecoveryClaim(context.Background(), claim.CaptureAuthorityID)
	if err != nil || !reflect.DeepEqual(got, claim) {
		t.Fatalf("reopened=%#v err=%v", got, err)
	}
}

func TestArtifactBlobRejectsSourceMutationAndLeavesNoVisibleBlob(t *testing.T) {
	r := openStructuredRepository(t)
	_, claim, request := artifactBlobFixture(t, r, "artifact-drift", []byte("abcdef"))
	if err := r.PutRecoveryClaim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	changed := request.PreSourceIdentity
	changed.Digest = strings.Repeat("f", 64)
	request.Source = &artifactBlobSource{data: []byte("abcdef"), identities: []structuredapp.ArtifactSourceIdentity{request.PreSourceIdentity, changed}}
	if _, err := r.CommitArtifactBlob(context.Background(), request); !errors.Is(err, structuredapp.ErrArtifactChangedDuringCapture) {
		t.Fatalf("source drift err=%v", err)
	}
	if _, err := os.Stat(r.artifactBlobPath(request.BlobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("drift exposed final blob: %v", err)
	}
	entries, err := os.ReadDir(r.artifactBlobRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".artifact-stage-") {
			t.Fatalf("drift left staging dir: %s", entry.Name())
		}
	}
	if err := r.ReleaseBlobReservation(context.Background(), request.Reservation); err != nil {
		t.Fatal(err)
	}
}

func TestBlobBudgetReservationsAreConcurrentAndProtectControlReserve(t *testing.T) {
	r := openStructuredRepository(t)
	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	r.limits.ControlReserve = 4096
	const reservationBytes = int64(1024)
	const slots = 3
	r.limits.MaxTotalState = exact + r.limits.ControlReserve + reservationBytes*slots

	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes []structuredapp.BlobByteReservation
	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("%064x", i)
			reservation, reserveErr := r.ReserveBlobBytes(context.Background(), id, reservationBytes)
			if reserveErr == nil {
				mu.Lock()
				successes = append(successes, reservation)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(successes) != slots {
		t.Fatalf("successful reservations=%d want=%d", len(successes), slots)
	}
	if _, err := r.ReserveBlobBytes(context.Background(), strings.Repeat("a", 64), 1); err == nil {
		t.Fatal("reservation consumed control reserve")
	}
	if err := r.ReleaseBlobReservation(context.Background(), successes[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReserveBlobBytes(context.Background(), strings.Repeat("b", 64), reservationBytes); err != nil {
		t.Fatalf("released capacity not reusable: %v", err)
	}
}

func TestBlobBudgetReservationContributesToEffectiveAdmissionState(t *testing.T) {
	r := openStructuredRepository(t)
	exact, err := r.scanStateBytes()
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := r.ReserveBlobBytes(context.Background(), strings.Repeat("c", 64), 2048)
	if err != nil {
		t.Fatal(err)
	}
	defer r.ReleaseBlobReservation(context.Background(), reservation)
	_, effective, err := r.admissionCounters()
	if err != nil {
		t.Fatal(err)
	}
	if effective < exact+reservation.Bytes {
		t.Fatalf("effective state=%d exact=%d reservation=%d", effective, exact, reservation.Bytes)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

var _ = core.ArtifactBlobRef{}
