package store

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestAbandonedReceiptPreservesPersistentV4Identity(t *testing.T) {
	reservation := operation.Reservation{
		SchemaVersion:                 4,
		OperationID:                   "persistent-abandon-op",
		SessionID:                     "persistent-abandon-session",
		RequestFingerprint:            strings.Repeat("a", 64),
		ExecutionFingerprint:          strings.Repeat("b", 64),
		ObservationBindingFingerprint: strings.Repeat("c", 64),
		ExecutionMode:                 operation.ExecutionModeShell,
		Executable:                    "/bin/sh",
		Command:                       "sleep 10",
		CWD:                           "/tmp",
		Shell:                         "/bin/sh",
		TimeoutMS:                     5000,
		StdinMode:                     operation.StdinModeClosed,
		TimeoutSource:                 "requested",
		StdinModeSource:               "requested",
		Persistent:                    true,
		SessionName:                   "dev-server",
	}
	snap := session.Snapshot{SchemaVersion: 1, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), State: session.Running}
	got := abandonedReceipt(snap, reservation, true, "new-daemon")
	if got.SchemaVersion != 4 || !got.Persistent || got.SessionName != reservation.SessionName {
		t.Fatalf("persistent identity lost: %#v", got)
	}
	if got.TimeoutMS != 5000 || got.TimeoutSource != "requested" || got.StdinMode != string(operation.StdinModeClosed) || got.StdinModeSource != "requested" {
		t.Fatalf("persistent policy provenance lost: %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("abandoned v4 receipt invalid: %v", err)
	}
}
