package store

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestReserveOperationIsIdempotent(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op1", SessionID: "s1", Fingerprint: "abc", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d1"}
	var wg sync.WaitGroup
	created := 0
	var mu sync.Mutex
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, result := r.ReserveOperation(context.Background(), res)
			if result.Err != nil {
				t.Error(result.Err)
				return
			}
			if fresh {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created=%d", created)
	}
	conflict := res
	conflict.Fingerprint = "other"
	if _, _, got := r.ReserveOperation(context.Background(), conflict); got.Err == nil {
		t.Fatal("expected conflict")
	}
}

func TestOutputCursorAndLimit(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 5, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "x", Command: "x", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d"}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.AppendOutput(context.Background(), "s", []byte("hello")); got.Err != nil {
		t.Fatal(got.Err)
	}
	b, next, err := r.ReadOutput(context.Background(), "s", 1, 2)
	if err != nil || string(b) != "el" || next != 3 {
		t.Fatalf("%q %d %v", b, next, err)
	}
	if _, got := r.AppendOutput(context.Background(), "s", []byte("!")); got.Err == nil {
		t.Fatal("limit not enforced")
	}
}

func TestObservationAdmissionRunningAndOutputSequencesAreExactlyOnce(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 16, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "obs-op", SessionID: "obs-session", Fingerprint: "fp", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon"}
	_, created, admission := r.ReserveOperation(context.Background(), res)
	if admission.Err != nil || !created || admission.ObservationSeq != 1 {
		t.Fatalf("admission created=%v result=%#v", created, admission)
	}
	_, created, replay := r.ReserveOperation(context.Background(), res)
	if replay.Err != nil || created || replay.ObservationSeq != 0 {
		t.Fatalf("replay created=%v result=%#v", created, replay)
	}

	processStart := r.PrepareProcessStartedObservation(context.Background(), "obs-op", "obs-session")
	if processStart.Err != nil || processStart.ObservationSeq != 2 {
		t.Fatalf("process start prepare=%#v", processStart)
	}
	running := session.Snapshot{SchemaVersion: 1, OperationID: "obs-op", SessionID: "obs-session", DaemonIncarnation: "daemon", State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := r.AdvanceSession(context.Background(), running); got.Err != nil || got.ObservationSeq != 0 {
		t.Fatalf("running result=%#v", got)
	}
	if got := r.CommitObservationSequence(context.Background(), processStart.ObservationSeq); got.Err != nil {
		t.Fatalf("process start commit=%#v", got)
	}
	if got := r.AdvanceSession(context.Background(), running); got.Err != nil || got.ObservationSeq != 0 {
		t.Fatalf("running replay=%#v", got)
	}
	if n, got := r.AppendOutput(context.Background(), "obs-session", []byte("hello")); n != 5 || got.Err != nil || got.ObservationSeq != 3 {
		t.Fatalf("append n=%d result=%#v", n, got)
	}
	if n, got := r.AppendOutput(context.Background(), "obs-session", []byte("!")); n != 1 || got.Err != nil || got.ObservationSeq != 4 {
		t.Fatalf("append2 n=%d result=%#v", n, got)
	}

	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 4 {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
	wantKinds := []observation.EventKind{observation.EventOperationAdmitted, observation.EventProcessStarted, observation.EventOutputAvailable, observation.EventOutputAvailable}
	wantSubjects := []string{"operation:obs-op", "session:obs-session:started", "output:obs-session:0:5", "output:obs-session:5:6"}
	for i, got := range obligations {
		if got.ChangeSeq != observation.ChangeSeq(i+1) || got.State != observation.ObligationCommitted || got.Kind != wantKinds[i] || got.SubjectRef != wantSubjects[i] {
			t.Fatalf("obligation[%d]=%#v", i, got)
		}
	}
}

func TestObservationFailedOutputAppendAbortsPreparedObligation(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 16, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "output-fail-op", SessionID: "output-fail-session", Fingerprint: "fp", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon"}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	output := filepath.Join(r.root, "sessions", "output-fail-session", "output.log")
	if err := os.WriteFile(output, nil, 0400); err != nil {
		t.Fatal(err)
	}
	if _, got := r.AppendOutput(context.Background(), "output-fail-session", []byte("x")); got.Err == nil {
		t.Fatal("read-only output append unexpectedly succeeded")
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 2 || obligations[1].State != observation.ObligationAborted || obligations[1].Kind != observation.EventOutputAvailable {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
}
