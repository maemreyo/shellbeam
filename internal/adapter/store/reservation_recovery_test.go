package store

import (
	"context"
	"errors"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestReserveOperationRepairsMetadataFromCommittedOperation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	res := operation.Reservation{
		SchemaVersion: 1, OperationID: "committed-op", SessionID: "committed-session",
		Fingerprint: "fingerprint", Command: "true", CWD: "/", Shell: "/bin/sh",
		DaemonIncarnation: "daemon-a", CreatedAt: time.Now().UTC(),
	}
	operationPath := filepath.Join(root, "operations", string(res.OperationID)+".json")
	if got := atomicCreateJSON(operationPath, res); got.Err != nil {
		t.Fatal(got.Err)
	}

	r = openRecoveryRepository(t, root)
	stored, created, got := r.ReserveOperation(context.Background(), res)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if created {
		t.Fatal("committed operation was authorized as a new start")
	}
	if stored.SessionID != res.SessionID {
		t.Fatalf("session = %q", stored.SessionID)
	}
	snap, err := r.LoadSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != session.Starting || snap.OperationID != string(res.OperationID) || snap.SessionID != string(res.SessionID) {
		t.Fatalf("repaired snapshot = %#v", snap)
	}
}

func TestAbandonUnresolvedClosesLegacyOrphanStartingMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	sessionDir := filepath.Join(root, "sessions", "orphan-session")
	if err := mkdirPrivate(sessionDir); err != nil {
		t.Fatal(err)
	}
	snap := session.Snapshot{
		SchemaVersion: 1, OperationID: "orphan-op", SessionID: "orphan-session",
		DaemonIncarnation: "dead-daemon", State: session.Starting,
		OutputAvailable: true, UpdatedAt: time.Now().UTC(),
	}
	if got := atomicJSON(filepath.Join(sessionDir, "metadata.json"), snap); got.Err != nil {
		t.Fatal(got.Err)
	}

	r = openRecoveryRepository(t, root)
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	repaired, err := r.LoadSession(context.Background(), "orphan-session")
	if err != nil {
		t.Fatal(err)
	}
	if repaired.State != session.Abandoned || repaired.Outcome != session.Ambiguous {
		t.Fatalf("snapshot = %#v", repaired)
	}
	rec, err := r.LoadReceipt(context.Background(), "orphan-session")
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.OperationID != "orphan-op" {
		t.Fatalf("receipt = %#v", rec)
	}
}

func openRecoveryRepository(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{
		MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mkdirPrivate(path string) error {
	return os.Mkdir(path, 0700)
}

func TestAmbiguousSessionMetadataAdmissionFailureFinalizesAndFreesCapacity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, err := Open(root, Limits{MaxSessions: 1, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	first := operation.Reservation{
		SchemaVersion: 1, OperationID: "metadata-ambiguous", SessionID: "metadata-ambiguous-session",
		Fingerprint: "fingerprint-1", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon-a",
	}
	r.writer = failAtomicWriterNth("replace.dir_sync", 2)
	_, created, got := r.ReserveOperation(context.Background(), first)
	if got.Err == nil || created || got.Durability != app.AmbiguousChange {
		t.Fatalf("created=%v result=%#v", created, got)
	}
	snap, err := r.LoadSession(context.Background(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != session.Abandoned || snap.Outcome != session.Ambiguous {
		t.Fatalf("ambiguous admission snapshot=%#v", snap)
	}
	rec, err := r.LoadReceipt(context.Background(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "admission_metadata_ambiguous" {
		t.Fatalf("ambiguous admission receipt=%#v", rec)
	}

	r.writer = atomicWriter{}
	second := operation.Reservation{
		SchemaVersion: 1, OperationID: "metadata-after-ambiguous", SessionID: "metadata-after-ambiguous-session",
		Fingerprint: "fingerprint-2", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon-a",
	}
	_, created, got = r.ReserveOperation(context.Background(), second)
	if got.Err != nil || !created {
		t.Fatalf("capacity remained occupied after ambiguous metadata failure: created=%v result=%#v", created, got)
	}
}

func TestObservationCanonicalAdmissionFailureAbortsThenRetryUsesNewSequence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	res := operation.Reservation{SchemaVersion: 1, OperationID: "admission-fault", SessionID: "admission-fault-session", Fingerprint: "fp", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon"}
	r.writer = failAtomicWriterNth("create.link", 2)
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err == nil || created {
		t.Fatalf("created=%v result=%#v", created, got)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationAborted {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
	r.writer = atomicWriter{}
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created || got.ObservationSeq != 2 {
		t.Fatalf("retry created=%v result=%#v", created, got)
	}
	obligations, err = r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 2 || obligations[1].State != observation.ObligationCommitted {
		t.Fatalf("retry obligations=%#v err=%v", obligations, err)
	}
}

func TestObservationPreparedCanonicalSubjectsReconcileAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	res := operation.Reservation{SchemaVersion: 1, OperationID: "reconcile-op", SessionID: "reconcile-session", Fingerprint: "fp", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "old-daemon"}

	r.writer = atomicWriter{}
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	processStart := r.PrepareProcessStartedObservation(context.Background(), string(res.OperationID), string(res.SessionID))
	if processStart.Err != nil {
		t.Fatalf("process prepare=%#v", processStart)
	}
	running := session.Snapshot{SchemaVersion: 1, OperationID: string(res.OperationID), SessionID: string(res.SessionID), DaemonIncarnation: "old-daemon", State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := r.AdvanceSession(context.Background(), running); got.Err != nil {
		t.Fatalf("running=%#v", got)
	}
	r.writer = failAtomicWriterNth("replace.rename", 1)
	if _, got := r.AppendOutput(context.Background(), res.SessionID, []byte("abc")); got.Err != nil {
		t.Fatalf("output=%#v", got)
	}
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: string(res.OperationID), SessionID: string(res.SessionID), Fingerprint: res.Fingerprint, DaemonIncarnation: "old-daemon", State: session.Completed, Outcome: session.Success, OutputBytes: 3, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	r.writer = failAtomicWriterNth("replace.rename", 1)
	if got := r.PublishTerminal(context.Background(), rec); got.Err != nil {
		t.Fatalf("terminal=%#v", got)
	}

	before, err := r.ListObservationObligations(context.Background(), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	prepared := 0
	for _, item := range before {
		if item.State == observation.ObligationPrepared {
			prepared++
		}
	}
	if prepared < 3 {
		t.Fatalf("prepared before restart=%d obligations=%#v", prepared, before)
	}

	r = openRecoveryRepository(t, root)
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	after, err := r.ListObservationObligations(context.Background(), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range after {
		if item.State == observation.ObligationPrepared {
			t.Fatalf("prepared obligation survived reconciliation: %#v", item)
		}
	}
}

func failAtomicWriterNth(point string, nth int) atomicWriter {
	seen := 0
	return atomicWriter{fail: func(got string) error {
		if got == point {
			seen++
			if seen == nth {
				return errors.New("injected nth persistence fault: " + point)
			}
		}
		return nil
	}}
}

func TestObservationReconciliationRejectsUnsafeSubjectRefs(t *testing.T) {
	cases := []struct {
		kind    observation.EventKind
		subject string
	}{
		{observation.EventOperationAdmitted, "operation:../escape"},
		{observation.EventProcessStarted, "session:../escape:started"},
		{observation.EventOutputAvailable, "output:..:0:1"},
		{observation.EventOutputAvailable, "output:s:not-a-number:2"},
		{observation.EventProcessTerminal, "receipt:../escape"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind)+"_"+tc.subject, func(t *testing.T) {
			r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
			prepared, result := r.PrepareObservation(context.Background(), observation.PrepareRequest{Kind: tc.kind, SubjectRef: tc.subject})
			if result.Err != nil || prepared.Obligation.State != observation.ObligationPrepared {
				t.Fatalf("prepare=%#v result=%#v", prepared, result)
			}
			if err := r.reconcilePreparedExecutionObservations(context.Background()); err == nil {
				t.Fatalf("unsafe subject %q reconciled without error", tc.subject)
			}
		})
	}
}
