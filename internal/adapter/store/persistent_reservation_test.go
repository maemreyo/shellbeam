package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestSchemaV4PersistentReservationValidatesShellAndRejectsMissingPersistentIdentity(t *testing.T) {
	base := operation.Reservation{
		SchemaVersion:        4,
		OperationID:          "op-persistent-v4",
		SessionID:            "session-persistent-v4",
		RequestFingerprint:   strings.Repeat("a", 64),
		ExecutionFingerprint: strings.Repeat("b", 64),
		ExecutionMode:        operation.ExecutionModeShell,
		Executable:           "/bin/sh",
		Command:              "sleep 10",
		CWD:                  "/tmp",
		Shell:                "/bin/sh",
		Persistent:           true,
		SessionName:          "dev-server",
		DaemonIncarnation:    "daemon",
	}
	if err := validateReservation(base); err != nil {
		t.Fatalf("valid persistent shell reservation rejected: %v", err)
	}
	missing := base
	missing.Persistent = false
	if err := validateReservation(missing); err == nil {
		t.Fatal("schema v4 reservation without persistent intent accepted")
	}
	badName := base
	badName.SessionName = "../dev"
	if err := validateReservation(badName); err == nil {
		t.Fatal("schema v4 reservation with invalid name accepted")
	}
}

func TestSchemaV4PersistentTypedBindingCommitsAgainstPersistentClaim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	claim := validTypedIntentClaim(t, "typed-persistent-v4")
	claim.Intent.Persistent = true
	claim.Intent.SessionName = "typed-dev"
	fingerprint, err := claim.Intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	claim.RequestFingerprint = fingerprint
	if _, created, got := r.ReserveTypedIntent(t.Context(), claim); got.Err != nil || !created {
		t.Fatalf("claim created=%v result=%#v", created, got)
	}
	want := validTypedReservation(t, claim, "typed-persistent-session")
	want.SchemaVersion = 4
	want.Persistent = true
	want.SessionName = claim.Intent.SessionName
	if _, created, got := r.CommitTypedBinding(t.Context(), claim.OperationID, want); got.Err != nil || !created {
		t.Fatalf("persistent typed commit created=%v result=%#v", created, got)
	}
}
