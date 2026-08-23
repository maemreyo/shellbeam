package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestDelegatedCaptureTruthStartsCompleteAndPrivateOmissionIsMonotonic(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-capture-private", "session-capture-private", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_capture")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}

	truth, err := r.LoadDelegatedCaptureTruth(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !truth.OutputComplete || truth.Quality != receipt.CaptureComplete || len(truth.Reasons) != 0 {
		t.Fatalf("initial=%#v", truth)
	}

	truth, got := r.MarkDelegatedCaptureReason(context.Background(), res.SessionID, receipt.CaptureReasonPrivateIntervalsOmitted)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if truth.OutputComplete || truth.Quality != receipt.CapturePartial || !reflect.DeepEqual(truth.Reasons, []receipt.CaptureReason{receipt.CaptureReasonPrivateIntervalsOmitted}) {
		t.Fatalf("private=%#v", truth)
	}
	truth, got = r.MarkDelegatedCaptureReason(context.Background(), res.SessionID, receipt.CaptureReasonPrivateIntervalsOmitted)
	if got.Err != nil || truth.Quality != receipt.CapturePartial || len(truth.Reasons) != 1 {
		t.Fatalf("idempotent=%#v err=%v", truth, got.Err)
	}

	truth, got = r.MarkDelegatedCaptureReason(context.Background(), operation.SessionID(res.SessionID), receipt.CaptureReasonTransportGap)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	truth, got = r.MarkDelegatedCaptureReason(context.Background(), res.SessionID, receipt.CaptureReasonProviderLost)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	want := []receipt.CaptureReason{receipt.CaptureReasonPrivateIntervalsOmitted, receipt.CaptureReasonTransportGap, receipt.CaptureReasonProviderLost}
	if truth.OutputComplete || truth.Quality != receipt.CaptureIncomplete || !reflect.DeepEqual(truth.Reasons, want) {
		t.Fatalf("combined=%#v want=%v", truth, want)
	}
}

func TestDelegatedCaptureTruthPersistsAcrossRepositoryReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 8)
	limits := r.limits
	res := task4DelegatedReservation("op-capture-reopen", "session-capture-reopen", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_capture_reopen")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.MarkDelegatedCaptureReason(context.Background(), res.SessionID, receipt.CaptureReasonPrivateIntervalsOmitted); got.Err != nil {
		t.Fatal(got.Err)
	}

	reopened, err := Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	truth, err := reopened.LoadDelegatedCaptureTruth(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if truth.Quality != receipt.CapturePartial || truth.OutputComplete || !reflect.DeepEqual(truth.Reasons, []receipt.CaptureReason{receipt.CaptureReasonPrivateIntervalsOmitted}) {
		t.Fatalf("reopened=%#v", truth)
	}
}
