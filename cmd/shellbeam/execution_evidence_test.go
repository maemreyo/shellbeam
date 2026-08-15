package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestExecutionEvidenceRuntimeRecoversDurableTerminalCandidate(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	contract := &core.Contract{VerificationKind: core.VerificationTest}
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: "runtime-recover", SessionID: "runtime-recover-session", RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "old", Evidence: contract, CreatedAt: now}
	if _, created, result := store.ReserveOperation(context.Background(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 2, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, ObservationBindingFingerprint: reservation.ObservationBindingFingerprint, DaemonIncarnation: "old", ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/", State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}, Evidence: contract}
	if result := store.PublishTerminal(context.Background(), rec); result.Err != nil {
		t.Fatal(result.Err)
	}

	runtime, err := newExecutionEvidenceRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.startRecovery(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if record, found, findErr := store.FindEvidenceByOperation(context.Background(), reservation.OperationID); findErr != nil {
			t.Fatal(findErr)
		} else if found {
			if record.Result != core.ResultPass || record.VerificationKind != core.VerificationTest {
				t.Fatalf("record=%#v", record)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, found, err := store.FindEvidenceByOperation(context.Background(), reservation.OperationID); err != nil || !found {
		t.Fatalf("recovered found=%v err=%v", found, err)
	}
	if err := runtime.shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.ListEvidenceCandidates(context.Background(), 8)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

func TestEvidenceWorkerProxyRejectsSchedulingBeforeBind(t *testing.T) {
	proxy := &evidenceWorkerProxy{}
	if err := proxy.ScheduleTerminal(context.Background(), receipt.Receipt{}); err == nil {
		t.Fatal("unbound evidence proxy accepted scheduling")
	}
}
