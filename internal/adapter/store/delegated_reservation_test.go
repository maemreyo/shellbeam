package store

import (
	"context"
	"errors"
	"testing"

	delegatedsession "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func delegatedReservation() operation.Reservation {
	return operation.Reservation{
		SchemaVersion:                 5,
		OperationID:                   "delegated-op",
		SessionID:                     "delegated-session",
		RequestFingerprint:            "delegated-request",
		ExecutionFingerprint:          "delegated-execution",
		ObservationBindingFingerprint: "delegated-observation",
		SessionMode:                   delegatedsession.ModeDelegatedInteractive,
		AuthorityEpoch:                1,
		ExecutionMode:                 operation.ExecutionModeShell,
		Executable:                    "/bin/sh",
		Command:                       "printf hi",
		CWD:                           "/tmp",
		Shell:                         "/bin/sh",
		SessionName:                   "agent-shell",
		DaemonIncarnation:             "daemon",
	}
}

func TestDelegatedReservationSchema5RoundTripAndReplay(t *testing.T) {
	r := admissionRepository(t, 4)
	base := delegatedReservation()
	stored, created, result := r.ReserveOperation(context.Background(), base)
	if result.Err != nil || !created {
		t.Fatalf("create stored=%#v created=%v result=%#v", stored, created, result)
	}
	loaded, err := r.LoadOperation(context.Background(), base.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 5 || loaded.SessionMode != delegatedsession.ModeDelegatedInteractive || loaded.AuthorityEpoch != 1 || loaded.SessionName != base.SessionName {
		t.Fatalf("delegated round trip=%#v", loaded)
	}

	retry := base
	retry.SessionID = "retry-session-must-not-win"
	stored, created, result = r.ReserveOperation(context.Background(), retry)
	if result.Err != nil || created || stored.SessionID != base.SessionID {
		t.Fatalf("replay stored=%#v created=%v result=%#v", stored, created, result)
	}

	changedName := base
	changedName.SessionName = "agent-shell-2"
	if _, _, got := r.ReserveOperation(context.Background(), changedName); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("session name conflict=%v", got.Err)
	}
	changedMode := base
	changedMode.SessionMode = ""
	if _, _, got := r.ReserveOperation(context.Background(), changedMode); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("session mode conflict=%v", got.Err)
	}
	changedEpoch := base
	changedEpoch.AuthorityEpoch = 2
	if _, _, got := r.ReserveOperation(context.Background(), changedEpoch); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("epoch conflict=%v", got.Err)
	}
}

func TestDelegatedReservationSchema5RejectsInvalidAuthorityAndOrdinaryEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*operation.Reservation){
		"tty":        func(v *operation.Reservation) { v.TTY = true },
		"persistent": func(v *operation.Reservation) { v.Persistent = true },
		"mode":       func(v *operation.Reservation) { v.SessionMode = "future_mode" },
		"epoch_zero": func(v *operation.Reservation) { v.AuthorityEpoch = 0 },
		"epoch_two":  func(v *operation.Reservation) { v.AuthorityEpoch = 2 },
		"evidence": func(v *operation.Reservation) {
			v.Evidence = &evidence.Contract{VerificationKind: evidence.VerificationTest}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := admissionRepository(t, 4)
			candidate := delegatedReservation()
			mutate(&candidate)
			if _, created, got := r.ReserveOperation(context.Background(), candidate); got.Err == nil || created {
				t.Fatalf("invalid schema5 reservation accepted: %#v result=%#v", candidate, got)
			}
		})
	}
}

func TestDirectAndDelegatedReservationsConflictInBothDirections(t *testing.T) {
	for _, delegatedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct_then_delegated", true: "delegated_then_direct"}[delegatedFirst], func(t *testing.T) {
			r := admissionRepository(t, 4)
			delegated := delegatedReservation()
			direct := delegated
			direct.SchemaVersion = 2
			direct.SessionMode = ""
			direct.AuthorityEpoch = 0
			direct.SessionName = ""
			if delegatedFirst {
				reserveOK(t, r, delegated)
				if _, _, got := r.ReserveOperation(context.Background(), direct); !errors.Is(got.Err, failure.OperationConflict) {
					t.Fatalf("delegated->direct conflict=%v", got.Err)
				}
				return
			}
			reserveOK(t, r, direct)
			if _, _, got := r.ReserveOperation(context.Background(), delegated); !errors.Is(got.Err, failure.OperationConflict) {
				t.Fatalf("direct->delegated conflict=%v", got.Err)
			}
		})
	}
}

func TestDelegatedTypedReservationSchema5RoundTrip(t *testing.T) {
	intent := operation.TypedRequestIntent{
		WorkspaceID:      "ws_01K00000000000000000000000",
		ProjectCommandID: "test_package",
		Params:           map[string]string{"package": "./internal/app"},
		TimeoutMS:        5000,
		SessionMode:      delegatedsession.ModeDelegatedInteractive,
		SessionName:      "typed-shell",
	}
	fingerprint, err := intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	claim := operation.TypedIntentClaim{
		SchemaVersion:      operation.TypedIntentClaimSchemaVersion,
		OperationID:        "delegated-typed-op",
		RequestFingerprint: fingerprint,
		Intent:             intent,
	}
	reservation := validTypedReservation(t, claim, "delegated-typed-session")
	reservation.SchemaVersion = 5
	reservation.SessionMode = delegatedsession.ModeDelegatedInteractive
	reservation.AuthorityEpoch = 1
	reservation.SessionName = intent.SessionName

	r := admissionRepository(t, 4)
	stored, created, result := r.ReserveOperation(context.Background(), reservation)
	if result.Err != nil || !created {
		t.Fatalf("typed delegated create stored=%#v created=%v result=%#v", stored, created, result)
	}
	loaded, err := r.LoadOperation(context.Background(), reservation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 5 || loaded.ProjectCommand == nil || loaded.SessionMode != delegatedsession.ModeDelegatedInteractive || loaded.AuthorityEpoch != 1 {
		t.Fatalf("typed delegated round trip=%#v", loaded)
	}
	if loaded.EvidenceEligible() {
		t.Fatal("typed delegated reservation became ordinary evidence eligible")
	}
}
