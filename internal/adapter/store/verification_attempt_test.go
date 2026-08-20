package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func storeAttempt(c byte, reason evidence.RerunReason) *evidence.VerificationAttemptIntent {
	return &evidence.VerificationAttemptIntent{RerunOfEvidenceID: "ev_" + strings.Repeat(string(c), 64), RerunReason: reason}
}

func rawAttemptReservation(t *testing.T, id string, attempt *evidence.VerificationAttemptIntent) operation.Reservation {
	t.Helper()
	contract := evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	obs, err := (operation.ObservationBinding{ActivityID: "activity-attempt", Evidence: &contract, VerificationAttempt: attempt}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return operation.Reservation{
		SchemaVersion: 2, OperationID: operation.ID(id), ActivityID: "activity-attempt", SessionID: operation.SessionID(id + "-session"),
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: obs,
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "daemon",
		Evidence: &contract, VerificationAttempt: attempt, CreatedAt: time.Unix(1, 0).UTC(),
	}
}

func TestVerificationAttemptRawReservationReplayAndConflict(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	first := rawAttemptReservation(t, "attempt-raw", storeAttempt('a', evidence.RerunDiagnoseFlake))
	stored, created, result := r.ReserveOperation(context.Background(), first)
	if result.Err != nil || !created || stored.VerificationAttempt == nil {
		t.Fatalf("first stored=%#v created=%v result=%#v", stored, created, result)
	}
	stored, created, result = r.ReserveOperation(context.Background(), first)
	if result.Err != nil || created || stored.VerificationAttempt == nil || stored.VerificationAttempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatalf("replay stored=%#v created=%v result=%#v", stored, created, result)
	}
	changed := rawAttemptReservation(t, "attempt-raw", storeAttempt('a', evidence.RerunFlakeQualification))
	if _, _, got := r.ReserveOperation(context.Background(), changed); !errors.Is(got.Err, failure.OperationMetadataConflict) {
		t.Fatalf("changed attempt result=%#v", got)
	}
}

func TestVerificationAttemptRawReservationRejectsUnboundAttempt(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	attempt := storeAttempt('a', evidence.RerunDiagnoseFlake)
	want := rawAttemptReservation(t, "attempt-unbound", nil)
	want.VerificationAttempt = attempt // fingerprint still binds nil attempt
	if _, _, got := r.ReserveOperation(context.Background(), want); got.Err == nil {
		t.Fatal("reservation accepted attempt not bound by observation fingerprint")
	}
}

func TestVerificationAttemptTypedClaimBindsReservation(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	claim := validTypedIntentClaim(t, "attempt-typed")
	claim.Intent.VerificationAttempt = storeAttempt('a', evidence.RerunFlakeQualification)
	fp, err := claim.Intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	claim.RequestFingerprint = fp
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatalf("claim created=%v result=%#v", created, got)
	}
	want := validTypedReservation(t, claim, "attempt-typed-session")
	want.VerificationAttempt = storeAttempt('a', evidence.RerunFlakeQualification)
	stored, created, got := r.CommitTypedBinding(context.Background(), claim.OperationID, want)
	if got.Err != nil || !created || stored.VerificationAttempt == nil {
		t.Fatalf("typed commit stored=%#v created=%v result=%#v", stored, created, got)
	}

	otherClaim := claim
	otherClaim.Intent.VerificationAttempt = storeAttempt('b', evidence.RerunFlakeQualification)
	otherFP, err := otherClaim.Intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	otherClaim.RequestFingerprint = otherFP
	if _, _, got := r.ReserveTypedIntent(context.Background(), otherClaim); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("typed relabel accepted: %#v", got)
	}
}

func TestVerificationAttemptTypedReservationMustMatchClaim(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	claim := validTypedIntentClaim(t, "attempt-typed-mismatch")
	claim.Intent.VerificationAttempt = storeAttempt('a', evidence.RerunDiagnoseFlake)
	fp, err := claim.Intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	claim.RequestFingerprint = fp
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatal(got.Err)
	}
	want := validTypedReservation(t, claim, "attempt-typed-mismatch-session")
	want.VerificationAttempt = storeAttempt('b', evidence.RerunDiagnoseFlake)
	if _, _, got := r.CommitTypedBinding(context.Background(), claim.OperationID, want); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("typed reservation attempt mismatch result=%#v", got)
	}
}

func TestVerificationAttemptPersistentV4ReplayCannotRelabel(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	first := rawAttemptReservation(t, "attempt-persistent-v4", storeAttempt('a', evidence.RerunDiagnoseFlake))
	first.SchemaVersion = 4
	first.Persistent = true
	stored, created, result := r.ReserveOperation(context.Background(), first)
	if result.Err != nil || !created || stored.SchemaVersion != 4 || stored.VerificationAttempt == nil {
		t.Fatalf("v4 first stored=%#v created=%v result=%#v", stored, created, result)
	}
	stored, created, result = r.ReserveOperation(context.Background(), first)
	if result.Err != nil || created || stored.VerificationAttempt == nil || stored.VerificationAttempt.RerunReason != evidence.RerunDiagnoseFlake {
		t.Fatalf("v4 replay stored=%#v created=%v result=%#v", stored, created, result)
	}
	changed := rawAttemptReservation(t, "attempt-persistent-v4", storeAttempt('a', evidence.RerunFlakeQualification))
	changed.SchemaVersion = 4
	changed.Persistent = true
	if _, _, got := r.ReserveOperation(context.Background(), changed); !errors.Is(got.Err, failure.OperationMetadataConflict) {
		t.Fatalf("v4 relabel accepted: %#v", got)
	}
}
