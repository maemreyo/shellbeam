package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestEvidenceRecordCreateOnceIndexesAndRestart(t *testing.T) {
	root := t.TempDir() + "/state"
	r := openEvidenceRepository(t, root)
	record := testEvidenceRecord()
	created, err := r.PutEvidenceRecord(context.Background(), record)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	got, err := r.LoadEvidenceRecord(context.Background(), record.EvidenceID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	byOperation, found, err := r.FindEvidenceByOperation(context.Background(), operation.ID(record.OperationID))
	if err != nil || !found || byOperation.EvidenceID != record.EvidenceID {
		t.Fatalf("by op=%#v found=%v err=%v", byOperation, found, err)
	}

	created, err = r.PutEvidenceRecord(context.Background(), record)
	if err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}

	reopened := openEvidenceRepository(t, root)
	byOperation, found, err = reopened.FindEvidenceByOperation(context.Background(), operation.ID(record.OperationID))
	if err != nil || !found || byOperation.EvidenceID != record.EvidenceID {
		t.Fatalf("restart by op=%#v found=%v err=%v", byOperation, found, err)
	}
}

func TestEvidenceRecordSameIdentityConflictingBytesFailsClosed(t *testing.T) {
	r := openEvidenceRepository(t, t.TempDir()+"/state")
	record := testEvidenceRecord()
	if created, err := r.PutEvidenceRecord(context.Background(), record); err != nil || !created {
		t.Fatalf("first=%v %v", created, err)
	}
	conflicting := record
	conflicting.Result = core.ResultFail
	if _, err := r.PutEvidenceRecord(context.Background(), conflicting); err == nil {
		t.Fatal("conflicting immutable evidence overwrite accepted")
	}
	got, err := r.LoadEvidenceRecord(context.Background(), record.EvidenceID)
	if err != nil || got.Result != core.ResultPass {
		t.Fatalf("canonical changed: %#v err=%v", got, err)
	}
}

func TestEvidenceRecordCommitsArtifactAndRecordedObservationObligationsAfterCanonicalState(t *testing.T) {
	r := openEvidenceRepository(t, t.TempDir()+"/state")
	record := testEvidenceRecord()
	record.Artifacts = []core.ArtifactObservation{{SchemaVersion: core.ArtifactSchemaVersion, Path: "dist/app", DeclaredKind: "file", Required: true, Exists: true, ObservedKind: "file", Status: core.ArtifactCurrent, Quality: core.ObservationComplete, ObservedAt: record.CompletedAt}}
	if created, err := r.PutEvidenceRecord(context.Background(), record); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(obligations) != 2 {
		t.Fatalf("obligations=%#v", obligations)
	}
	if obligations[0].Kind != observation.EventArtifactObserved || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("artifact obligation=%#v", obligations[0])
	}
	if obligations[1].Kind != observation.EventEvidenceRecorded || obligations[1].State != observation.ObligationCommitted {
		t.Fatalf("evidence obligation=%#v", obligations[1])
	}
	if _, err := r.LoadEvidenceRecord(context.Background(), record.EvidenceID); err != nil {
		t.Fatalf("committed event without canonical record: %v", err)
	}
}

func openEvidenceRepository(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func testEvidenceRecord() core.Record {
	now := time.Now().UTC()
	return core.Record{SchemaVersion: core.SchemaVersion, EvidenceID: "ev_" + strings.Repeat("a", 64), OperationID: "evidence-op", SessionID: "evidence-session", VerificationKind: core.VerificationTest, ContractDigest: strings.Repeat("b", 64), ReceiptDigest: strings.Repeat("c", 64), Terminal: core.TerminalResult{Authoritative: true, Outcome: session.Success}, Result: core.ResultPass, Source: core.SourceBinding{ObservationQuality: core.SourceQualityUnknown}, CompletedAt: now}
}

func TestEvidenceCandidateIndexOnlyTracksEligibleReservationsAndSurvivesRestart(t *testing.T) {
	root := t.TempDir() + "/state"
	r := openEvidenceRepository(t, root)
	plain := candidateReservation("plain-op", nil, nil)
	if _, _, result := r.ReserveOperation(context.Background(), plain); result.Err != nil {
		t.Fatal(result.Err)
	}
	explicit := candidateReservation("evidence-op-2", &core.Contract{VerificationKind: core.VerificationTest}, nil)
	if _, _, result := r.ReserveOperation(context.Background(), explicit); result.Err != nil {
		t.Fatal(result.Err)
	}
	intent := &operation.DeclaredIntent{Kind: operation.IntentKindBuild}
	withIntent := candidateReservation("intent-op", nil, intent)
	if _, _, result := r.ReserveOperation(context.Background(), withIntent); result.Err != nil {
		t.Fatal(result.Err)
	}

	candidates, err := r.ListEvidenceCandidates(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || string(candidates[0]) != "evidence-op-2" || string(candidates[1]) != "intent-op" {
		t.Fatalf("candidates=%#v", candidates)
	}

	reopened := openEvidenceRepository(t, root)
	candidates, err = reopened.ListEvidenceCandidates(context.Background(), 10)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("restart candidates=%#v err=%v", candidates, err)
	}
}

func candidateReservation(id string, contract *core.Contract, intent *operation.DeclaredIntent) operation.Reservation {
	now := time.Now().UTC()
	return operation.Reservation{SchemaVersion: 2, OperationID: operation.ID(id), SessionID: operation.SessionID(id + "-session"), RequestFingerprint: strings.Repeat("d", 64), ExecutionFingerprint: strings.Repeat("e", 64), ObservationBindingFingerprint: strings.Repeat("f", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/", Shell: "/bin/sh", Evidence: contract, Intent: intent, CreatedAt: now}
}

func TestEvidenceCandidateWriteFailureCannotCommitOperation(t *testing.T) {
	r := openEvidenceRepository(t, t.TempDir()+"/state")
	candidateDir := filepath.Join(r.root, "evidence", "candidates")
	if err := os.Chmod(candidateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(candidateDir, 0o700) })
	reservation := candidateReservation("candidate-write-fail", &core.Contract{VerificationKind: core.VerificationTest}, nil)
	if _, created, result := r.ReserveOperation(context.Background(), reservation); result.Err == nil || created {
		t.Fatalf("created=%v result=%#v", created, result)
	}
	if _, found, err := r.FindOperation(context.Background(), reservation.OperationID); err != nil || found {
		t.Fatalf("operation committed despite candidate failure: found=%v err=%v", found, err)
	}
}

func TestEvidenceIndexObligationsReadDirectBoundedSequenceRange(t *testing.T) {
	r := openEvidenceRepository(t, t.TempDir()+"/state")
	first := testEvidenceRecord()
	if created, err := r.PutEvidenceRecord(context.Background(), first); err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, err)
	}
	second := testEvidenceRecord()
	second.EvidenceID = "ev_" + strings.Repeat("d", 64)
	second.OperationID = "evidence-op-two"
	second.SessionID = "evidence-session-two"
	second.ReceiptDigest = strings.Repeat("e", 64)
	if created, err := r.PutEvidenceRecord(context.Background(), second); err != nil || !created {
		t.Fatalf("second created=%v err=%v", created, err)
	}
	high, err := r.ObservationHighWatermark(context.Background())
	if err != nil || high != 2 {
		t.Fatalf("high=%d err=%v", high, err)
	}
	firstPage, err := r.ListEvidenceIndexObligations(context.Background(), 0, high, 1)
	if err != nil || len(firstPage) != 1 || firstPage[0].ChangeSeq != 1 || firstPage[0].Kind != observation.EventEvidenceRecorded {
		t.Fatalf("first page=%#v err=%v", firstPage, err)
	}
	secondPage, err := r.ListEvidenceIndexObligations(context.Background(), 1, high, 1)
	if err != nil || len(secondPage) != 1 || secondPage[0].ChangeSeq != 2 || secondPage[0].Kind != observation.EventEvidenceRecorded {
		t.Fatalf("second page=%#v err=%v", secondPage, err)
	}
	if _, err := r.ListEvidenceIndexObligations(context.Background(), 0, high, core.MaxInspectScanRecords+1); err == nil {
		t.Fatal("oversized evidence index scan accepted")
	}
}
