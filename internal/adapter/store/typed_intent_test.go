package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

func TestReserveTypedIntentConcurrentSameClaimHasOneWinner(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	claim := validTypedIntentClaim(t, "typed-concurrent")
	var created atomic.Int32
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, won, result := r.ReserveTypedIntent(context.Background(), claim)
			if result.Err != nil || stored.RequestFingerprint != claim.RequestFingerprint {
				failed.Add(1)
				return
			}
			if won {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 || created.Load() != 1 {
		t.Fatalf("failed=%d created=%d", failed.Load(), created.Load())
	}
}

func TestReserveTypedIntentConflictReopenAndNoSessionMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	claim := validTypedIntentClaim(t, "typed-claim")
	stored, created, result := r.ReserveTypedIntent(context.Background(), claim)
	if result.Err != nil || !created || stored.RequestFingerprint != claim.RequestFingerprint {
		t.Fatalf("stored=%#v created=%v result=%#v", stored, created, result)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "sessions")); err != nil || len(entries) != 0 {
		t.Fatalf("claim created session metadata: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(root, "operations", string(claim.OperationID)+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claim created operation reservation: %v", err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 0 {
		t.Fatalf("claim created admission observation: obligations=%#v err=%v", obligations, err)
	}
	conflict := claim
	conflict.Intent.Params = map[string]string{"package": "./other"}
	fingerprint, err := conflict.Intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	conflict.RequestFingerprint = fingerprint
	if _, _, got := r.ReserveTypedIntent(context.Background(), conflict); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("conflict result=%#v", got)
	}

	r = openRecoveryRepository(t, root)
	found, ok, err := r.FindTypedIntent(context.Background(), claim.OperationID)
	if err != nil || !ok || found.RequestFingerprint != claim.RequestFingerprint || found.Intent.ProjectCommandID != claim.Intent.ProjectCommandID {
		t.Fatalf("reopened found=%#v ok=%v err=%v", found, ok, err)
	}
	stored, created, result = r.ReserveTypedIntent(context.Background(), claim)
	if result.Err != nil || created || stored.RequestFingerprint != claim.RequestFingerprint {
		t.Fatalf("replay stored=%#v created=%v result=%#v", stored, created, result)
	}
}

func TestCommitTypedBindingCreatesSchemaV3ReservationOnlyAfterClaim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	claim := validTypedIntentClaim(t, "typed-bind")
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatalf("claim created=%v result=%#v", created, got)
	}
	want := validTypedReservation(t, claim, "typed-bind-session")
	stored, created, result := r.CommitTypedBinding(context.Background(), claim.OperationID, want)
	if result.Err != nil || !created {
		t.Fatalf("commit stored=%#v created=%v result=%#v", stored, created, result)
	}
	if stored.SchemaVersion != 3 || stored.ProjectCommand == nil || stored.ProjectCommand.CommandID != claim.Intent.ProjectCommandID {
		t.Fatalf("stored=%#v", stored)
	}
	if _, err := r.LoadSession(context.Background(), want.SessionID); err != nil {
		t.Fatalf("session metadata missing after binding commit: %v", err)
	}
	loaded, err := r.LoadOperation(context.Background(), claim.OperationID)
	if err != nil || loaded.ProjectCommand == nil || loaded.ProjectCommand.ParameterFingerprint != want.ProjectCommand.ParameterFingerprint {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}

	replay := want
	replay.ExecutionFingerprint = strings.Repeat("e", 64)
	stored, created, result = r.CommitTypedBinding(context.Background(), claim.OperationID, replay)
	if result.Err != nil || created || stored.ExecutionFingerprint != want.ExecutionFingerprint {
		t.Fatalf("idempotent replay stored=%#v created=%v result=%#v", stored, created, result)
	}
}

func TestCommitTypedBindingRejectsMissingClaimClaimMismatchAndBindingConflict(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	claim := validTypedIntentClaim(t, "typed-conflict")
	want := validTypedReservation(t, claim, "typed-conflict-session")
	if _, _, got := r.CommitTypedBinding(context.Background(), claim.OperationID, want); got.Err == nil {
		t.Fatal("binding committed without durable typed claim")
	}
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatal(got.Err)
	}
	mismatch := want
	mismatch.RequestFingerprint = strings.Repeat("f", 64)
	if _, _, got := r.CommitTypedBinding(context.Background(), claim.OperationID, mismatch); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("claim mismatch=%#v", got)
	}
	if _, created, got := r.CommitTypedBinding(context.Background(), claim.OperationID, want); got.Err != nil || !created {
		t.Fatalf("first binding created=%v result=%#v", created, got)
	}
	conflict := want
	binding := *want.ProjectCommand
	binding.ResolvedArgv = []string{"go", "test", "./other"}
	params := append([]project.ParameterBinding(nil), binding.Parameters...)
	params[0].Value = "./other"
	binding.Parameters = params
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	binding.ParameterFingerprint = fingerprint
	conflict.ProjectCommand = &binding
	conflict.Argv = append([]string(nil), binding.ResolvedArgv...)
	if _, _, got := r.CommitTypedBinding(context.Background(), claim.OperationID, conflict); got.Err == nil {
		t.Fatal("conflicting frozen binding replay accepted")
	}
	loaded, err := r.LoadOperation(context.Background(), claim.OperationID)
	if err != nil || loaded.ProjectCommand == nil || loaded.ProjectCommand.ParameterFingerprint != want.ProjectCommand.ParameterFingerprint {
		t.Fatalf("conflict mutated stored operation: %#v err=%v", loaded, err)
	}
}

func TestTypedIntentFaultBoundariesRetryWithoutDuplicateClaim(t *testing.T) {
	createPoints := []string{"create.create_temp", "create.write", "create.file_sync", "create.close", "create.link", "create.open_dir", "create.dir_sync"}
	for _, point := range createPoints {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			r := openRecoveryRepository(t, root)
			claim := validTypedIntentClaim(t, "typed-fault")
			r.writer = failAtomicWriter(point)
			_, created, first := r.ReserveTypedIntent(context.Background(), claim)
			if first.Err == nil || created {
				t.Fatalf("first created=%v result=%#v", created, first)
			}
			path := filepath.Join(root, "typed-intents", string(claim.OperationID)+".json")
			_, statErr := os.Stat(path)
			committed := statErr == nil
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal(statErr)
			}
			r = openRecoveryRepository(t, root)
			stored, created, retry := r.ReserveTypedIntent(context.Background(), claim)
			if retry.Err != nil || stored.RequestFingerprint != claim.RequestFingerprint {
				t.Fatalf("retry stored=%#v created=%v result=%#v", stored, created, retry)
			}
			if created == committed {
				t.Fatalf("created=%v committed-before-retry=%v", created, committed)
			}
			if entries, err := os.ReadDir(filepath.Join(root, "sessions")); err != nil || len(entries) != 0 {
				t.Fatalf("claim retry created session metadata: entries=%v err=%v", entries, err)
			}
		})
	}
}

func validTypedIntentClaim(t *testing.T, operationID operation.ID) operation.TypedIntentClaim {
	t.Helper()
	intent := operation.TypedRequestIntent{
		WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package",
		Params: map[string]string{"package": "./internal/app"}, TimeoutMS: 5000,
	}
	fingerprint, err := intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return operation.TypedIntentClaim{
		SchemaVersion: operation.TypedIntentClaimSchemaVersion, OperationID: operationID,
		RequestFingerprint: fingerprint, Intent: intent, CreatedAt: time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC),
	}
}

func validTypedReservation(t *testing.T, claim operation.TypedIntentClaim, sessionID operation.SessionID) operation.Reservation {
	t.Helper()
	params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	binding := project.CommandBinding{
		SchemaVersion: project.BindingSchemaVersion, ManifestDigest: strings.Repeat("c", 64), ManifestSchemaVersion: project.ManifestSchemaV2,
		CommandID: claim.Intent.ProjectCommandID, ParameterFingerprint: fingerprint, Parameters: params,
		ResolvedArgv: []string{"go", "test", "./internal/app"}, LogicalCWD: ".", ResolvedCWD: "/repo",
	}
	return operation.Reservation{
		SchemaVersion: 3, OperationID: claim.OperationID, WorkspaceID: claim.Intent.WorkspaceID,
		LogicalCWD: binding.LogicalCWD, SessionID: sessionID,
		RequestFingerprint: claim.RequestFingerprint, ExecutionFingerprint: strings.Repeat("d", 64),
		ExecutionMode: operation.ExecutionModeArgv, Executable: "go", Argv: append([]string(nil), binding.ResolvedArgv...),
		CWD: binding.ResolvedCWD, TTY: claim.Intent.TTY, TimeoutMS: claim.Intent.TimeoutMS,
		DaemonIncarnation: "daemon", ProjectCommand: &binding,
	}
}

func TestAbandonUnresolvedTypedV3PreservesProjectCommandProvenance(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	claim := validTypedIntentClaim(t, "typed-abandon")
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatalf("claim created=%v result=%#v", created, got)
	}
	want := validTypedReservation(t, claim, "typed-abandon-session")
	if _, created, got := r.CommitTypedBinding(context.Background(), claim.OperationID, want); got.Err != nil || !created {
		t.Fatalf("commit created=%v result=%#v", created, got)
	}
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	rec, err := r.LoadReceipt(context.Background(), want.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SchemaVersion != 3 || rec.RequestFingerprint != want.RequestFingerprint || rec.ExecutionFingerprint != want.ExecutionFingerprint || rec.ProjectCommand == nil {
		t.Fatalf("abandoned typed receipt=%#v", rec)
	}
	if got, wantDigest := rec.ProjectCommand.ParameterFingerprint, want.ProjectCommand.ParameterFingerprint; got != wantDigest {
		t.Fatalf("project command fingerprint=%q want=%q", got, wantDigest)
	}
}
