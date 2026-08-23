package verification

import (
	"testing"
	"time"
)

func qref(kind, ref string) ResourceRef { return ResourceRef{Kind: kind, Ref: ref} }

func qualifiedQuiescence(status QuiescenceStatus, live, transferred, unexpected []ResourceRef) QuiescenceObservation {
	return QuiescenceObservation{
		SchemaVersion: 1,
		OperationID:   "op_quiescence",
		SessionID:     "session_quiescence",
		Status:        status,
		LiveResources: live,
		Transferred:   transferred,
		Unexpected:    unexpected,
		ObservedAt:    time.Unix(100, 0).UTC(),
		Quality:       QuiescenceQualityQualifiedLifecycle,
	}
}

func TestQuiescenceBlocksLeaksAndAllowsTypedTransfer(t *testing.T) {
	t.Run("qualified zero residue is complete", func(t *testing.T) {
		got := ReconcileQuiescence(QuiescenceInput{Lifecycle: ptrQuiescence(qualifiedQuiescence(QuiescenceComplete, nil, nil, nil))})
		if got.Status != QuiescenceComplete || len(got.Unexpected) != 0 {
			t.Fatalf("got=%#v", got)
		}
	})
	t.Run("untransferred live child is incomplete", func(t *testing.T) {
		live := []ResourceRef{qref(ResourceKindProcess, "pid:42")}
		got := ReconcileQuiescence(QuiescenceInput{Lifecycle: ptrQuiescence(qualifiedQuiescence(QuiescenceComplete, live, nil, nil))})
		if got.Status != QuiescenceIncomplete || len(got.Unexpected) != 1 {
			t.Fatalf("got=%#v", got)
		}
	})
	t.Run("typed persistent transfer subtracts exact ref", func(t *testing.T) {
		ref := qref(ResourceKindPersistentSession, "session_persistent")
		proof := qualifiedQuiescence(QuiescenceComplete, []ResourceRef{ref}, []ResourceRef{ref}, nil)
		got := ReconcileQuiescence(QuiescenceInput{Lifecycle: &proof, AllowedTransfers: []ResourceRef{ref}})
		if got.Status != QuiescenceComplete || len(got.Transferred) != 1 || len(got.Unexpected) != 0 {
			t.Fatalf("got=%#v", got)
		}
	})
	t.Run("unbound transfer cannot subtract live resource", func(t *testing.T) {
		ref := qref(ResourceKindPersistentSession, "session_unbound")
		proof := qualifiedQuiescence(QuiescenceComplete, []ResourceRef{ref}, []ResourceRef{ref}, nil)
		got := ReconcileQuiescence(QuiescenceInput{Lifecycle: &proof})
		if got.Status != QuiescenceIncomplete || len(got.Unexpected) != 1 || len(got.Transferred) != 0 {
			t.Fatalf("got=%#v", got)
		}
	})
	t.Run("arbitrary pid cannot be transferred", func(t *testing.T) {
		ref := qref(ResourceKindProcess, "pid:99")
		proof := qualifiedQuiescence(QuiescenceComplete, []ResourceRef{ref}, []ResourceRef{ref}, nil)
		got := ReconcileQuiescence(QuiescenceInput{Lifecycle: &proof, AllowedTransfers: []ResourceRef{ref}})
		if got.Status != QuiescenceIncomplete || len(got.Transferred) != 0 {
			t.Fatalf("got=%#v", got)
		}
	})
}

func TestQuiescenceNegativeAndUnknownEvidenceStayLiteral(t *testing.T) {
	if got := ReconcileQuiescence(QuiescenceInput{CleanupIncomplete: true}); got.Status != QuiescenceIncomplete {
		t.Fatalf("cleanup=%#v", got)
	}
	if got := ReconcileQuiescence(QuiescenceInput{}); got.Status != QuiescenceUnknown {
		t.Fatalf("missing lifecycle=%#v", got)
	}
	unavailable := qualifiedQuiescence(QuiescenceUnavailable, nil, nil, nil)
	unavailable.Quality = QuiescenceQualityUnavailable
	if got := ReconcileQuiescence(QuiescenceInput{Lifecycle: &unavailable}); got.Status != QuiescenceUnavailable {
		t.Fatalf("unavailable=%#v", got)
	}
}

func ptrQuiescence(v QuiescenceObservation) *QuiescenceObservation { return &v }
