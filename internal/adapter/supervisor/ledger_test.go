package supervisor

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestInputLedgerSurvivesReopenAndPreservesOffsetIdentity(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-input", "generation-input")
	limits := InputLimits{MaxRecords: 8, MaxMetadataBytes: 16 << 10, MaxQueuedBytes: 16}
	ledger, err := OpenInputLedger(layout, limits)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := ledger.AcceptChars(0, []byte("abc"))
	if err != nil || admission.Duplicate || !admission.NeedsDelivery || admission.NextOffset != 3 {
		t.Fatalf("admission=%#v err=%v", admission, err)
	}
	if err := ledger.MarkDelivered(admission.Record); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenInputLedger(layout, limits)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := reopened.AcceptChars(0, []byte("abc"))
	if err != nil || !duplicate.Duplicate || duplicate.NeedsDelivery || duplicate.NextOffset != 3 {
		t.Fatalf("duplicate=%#v err=%v", duplicate, err)
	}
	if _, err := reopened.AcceptChars(0, []byte("abd")); err == nil || err.Error() != "input_conflict" {
		t.Fatalf("changed retry err=%v", err)
	}
	if _, err := reopened.AcceptChars(4, []byte("x")); err == nil || err.Error() != "input_gap" {
		t.Fatalf("gap err=%v", err)
	}
	second, err := reopened.AcceptChars(3, []byte("d"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.MarkDelivered(second.Record); err != nil {
		t.Fatal(err)
	}
	eof, err := reopened.AcceptEOF(4)
	if err != nil || !eof.NeedsDelivery {
		t.Fatalf("eof=%#v err=%v", eof, err)
	}
	if err := reopened.MarkDelivered(eof.Record); err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.Snapshot()
	if snapshot.AcceptedBytes != 4 || snapshot.DeliveredBytes != 4 || !snapshot.EOFAccepted || !snapshot.EOFDelivered || snapshot.NextOffset != 4 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func TestInputLedgerHistoryExhaustionStillReplaysRecordedInput(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-input-cap", "generation-input-cap")
	ledger, err := OpenInputLedger(layout, InputLimits{MaxRecords: 1, MaxMetadataBytes: 4096, MaxQueuedBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	first, err := ledger.AcceptChars(0, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.MarkDelivered(first.Record); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AcceptChars(1, []byte("b")); !errors.Is(err, failure.PersistentInputHistoryExhausted) {
		t.Fatalf("new input after history exhaustion err=%v", err)
	}
	duplicate, err := ledger.AcceptChars(0, []byte("a"))
	if err != nil || !duplicate.Duplicate || duplicate.NeedsDelivery {
		t.Fatalf("recorded duplicate unavailable: %#v err=%v", duplicate, err)
	}
}

func TestKillLedgerReplaysSameIDAndRejectsConflictOrExhaustion(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-kill", "generation-kill")
	ledger, err := OpenKillLedger(layout, 2)
	if err != nil {
		t.Fatal(err)
	}
	attempt, send, err := ledger.Admit("kill-1", "TERM", false)
	if err != nil || !send || attempt.Attempted {
		t.Fatalf("attempt=%#v send=%v err=%v", attempt, send, err)
	}
	attempt.Attempted, attempt.Succeeded = true, true
	if err := ledger.Record(attempt); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenKillLedger(layout, 2)
	if err != nil {
		t.Fatal(err)
	}
	replay, send, err := reopened.Admit("kill-1", "TERM", false)
	if err != nil || send || !replay.Attempted || !replay.Succeeded {
		t.Fatalf("replay=%#v send=%v err=%v", replay, send, err)
	}
	if _, _, err := reopened.Admit("kill-1", "KILL", false); err == nil || err.Error() != "kill_conflict" {
		t.Fatalf("kill conflict err=%v", err)
	}
	if _, _, err := reopened.Admit("kill-2", "INT", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.Admit("kill-3", "TERM", false); !errors.Is(err, failure.PersistentKillHistoryExhausted) {
		t.Fatalf("kill history exhaustion err=%v", err)
	}
}
