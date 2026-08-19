package verification

import (
	"context"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

type quiescenceReceiptFake struct {
	rec receipt.Receipt
	err error
}

func (f quiescenceReceiptFake) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	return f.rec, f.err
}

type quiescencePersistentFake struct {
	page persistent.BindingPage
	err  error
}

func (f quiescencePersistentFake) ListPersistentBindings(context.Context, persistent.InspectRequest) (persistent.BindingPage, error) {
	return f.page, f.err
}

type quiescenceLifecycleFake struct {
	obs   core.QuiescenceObservation
	found bool
	err   error
}

func (f quiescenceLifecycleFake) QuiescenceForOperation(context.Context, string) (core.QuiescenceObservation, bool, error) {
	return f.obs, f.found, f.err
}

func lifecycleObservation(status core.QuiescenceStatus) core.QuiescenceObservation {
	quality := core.QuiescenceQualityQualifiedLifecycle
	if status == core.QuiescenceUnavailable {
		quality = core.QuiescenceQualityUnavailable
	}
	return core.QuiescenceObservation{SchemaVersion: 1, OperationID: "op_q", SessionID: "session_q", Status: status, ObservedAt: time.Unix(100, 0).UTC(), Quality: quality}
}

func TestQuiescenceSourceTreatsCleanupFailureAsAuthoritativeNegative(t *testing.T) {
	rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q", ResourceCleanup: &receipt.ResourceCleanup{Status: receipt.ResourceCleanupIncomplete, Reason: "cleanup_remove_failed"}}
	s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{}, quiescenceLifecycleFake{obs: lifecycleObservation(core.QuiescenceComplete), found: true})
	got, ok, err := s.Observe(context.Background(), "op_q", "session_q", "")
	if err != nil || !ok || got.Status != core.QuiescenceIncomplete || got.Quality != core.QuiescenceQualityReceiptNegative {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestQuiescenceSourceMissingLifecycleNeverProvesComplete(t *testing.T) {
	rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
	s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{}, quiescenceLifecycleFake{})
	got, ok, err := s.Observe(context.Background(), "op_q", "session_q", "")
	if err != nil || !ok || got.Status != core.QuiescenceUnknown {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestQuiescenceSourceSubtractsOnlyExactPersistentBinding(t *testing.T) {
	ref := core.ResourceRef{Kind: core.ResourceKindPersistentSession, Ref: "session_persistent"}
	obs := lifecycleObservation(core.QuiescenceComplete)
	obs.LiveResources = []core.ResourceRef{ref}
	obs.Transferred = []core.ResourceRef{ref}
	binding := persistent.Binding{SchemaVersion: persistent.SchemaVersion, SessionID: "session_persistent", OperationID: "op_server", WorkspaceID: "ws_01K00000000000000000000000", SessionName: "server", Persistent: true, Supervision: persistent.SupervisionPerSession, Continuity: persistent.ContinuityDaemonRestart, SupervisorGenerationID: "gen_owner", SupervisorEndpointRef: "endpoint_owner", Lifecycle: persistent.LifecycleLive, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC()}
	rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
	s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{page: persistent.BindingPage{Bindings: []persistent.Binding{binding}}}, quiescenceLifecycleFake{obs: obs, found: true})
	got, ok, err := s.Observe(context.Background(), "op_q", "session_q", binding.WorkspaceID)
	if err != nil || !ok || got.Status != core.QuiescenceComplete || len(got.Transferred) != 1 {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}

	s = NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{page: persistent.BindingPage{Bindings: []persistent.Binding{binding}}}, quiescenceLifecycleFake{obs: obs, found: true})
	got, _, err = s.Observe(context.Background(), "op_q", "session_q", "ws_01K11111111111111111111111")
	if err != nil || got.Status != core.QuiescenceIncomplete || len(got.Transferred) != 0 {
		t.Fatalf("wrong workspace transfer=%#v err=%v", got, err)
	}
}

func TestQuiescenceSourceProviderUnavailableStaysUnavailable(t *testing.T) {
	rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
	obs := lifecycleObservation(core.QuiescenceUnavailable)
	s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{}, quiescenceLifecycleFake{obs: obs, found: true})
	got, ok, err := s.Observe(context.Background(), "op_q", "session_q", "")
	if err != nil || !ok || got.Status != core.QuiescenceUnavailable {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestDelegatedOwnershipVerifiesIntegrationAssumptionWithoutProviderStress(t *testing.T) {
	t.Run("qualified lifecycle leaked process stays incomplete", func(t *testing.T) {
		obs := lifecycleObservation(core.QuiescenceComplete)
		obs.LiveResources = []core.ResourceRef{{Kind: core.ResourceKindProcess, Ref: "pid:4242"}}
		rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
		s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, nil, quiescenceLifecycleFake{obs: obs, found: true})
		got, ok, err := s.Observe(context.Background(), "op_q", "session_q", "ws_01K00000000000000000000000")
		if err != nil || !ok || got.Status != core.QuiescenceIncomplete || len(got.Unexpected) != 1 || got.Unexpected[0].Ref != "pid:4242" {
			t.Fatalf("qualified leak was not retained: got=%#v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("typed transfer without ownership coverage stays unknown", func(t *testing.T) {
		ref := core.ResourceRef{Kind: core.ResourceKindPersistentSession, Ref: "session_persistent"}
		obs := lifecycleObservation(core.QuiescenceComplete)
		obs.LiveResources, obs.Transferred = []core.ResourceRef{ref}, []core.ResourceRef{ref}
		rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
		s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, nil, quiescenceLifecycleFake{obs: obs, found: true})
		got, ok, err := s.Observe(context.Background(), "op_q", "session_q", "ws_01K00000000000000000000000")
		if err != nil || !ok || got.Status != core.QuiescenceUnknown || got.Quality != core.QuiescenceQualityUnavailable {
			t.Fatalf("uncovered transfer invented completion: got=%#v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("exact persistent ownership transfer can complete", func(t *testing.T) {
		ref := core.ResourceRef{Kind: core.ResourceKindPersistentSession, Ref: "session_persistent"}
		obs := lifecycleObservation(core.QuiescenceComplete)
		obs.LiveResources, obs.Transferred = []core.ResourceRef{ref}, []core.ResourceRef{ref}
		binding := persistent.Binding{SchemaVersion: persistent.SchemaVersion, SessionID: "session_persistent", OperationID: "op_server", WorkspaceID: "ws_01K00000000000000000000000", SessionName: "server", Persistent: true, Supervision: persistent.SupervisionPerSession, Continuity: persistent.ContinuityDaemonRestart, SupervisorGenerationID: "gen_owner", SupervisorEndpointRef: "endpoint_owner", Lifecycle: persistent.LifecycleLive, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC()}
		rec := receipt.Receipt{SchemaVersion: 5, OperationID: "op_q", SessionID: "session_q"}
		s := NewQuiescenceSource(quiescenceReceiptFake{rec: rec}, quiescencePersistentFake{page: persistent.BindingPage{Bindings: []persistent.Binding{binding}}}, quiescenceLifecycleFake{obs: obs, found: true})
		got, ok, err := s.Observe(context.Background(), "op_q", "session_q", binding.WorkspaceID)
		if err != nil || !ok || got.Status != core.QuiescenceComplete || len(got.Transferred) != 1 || len(got.Unexpected) != 0 {
			t.Fatalf("exact ownership transfer did not reconcile: got=%#v ok=%v err=%v", got, ok, err)
		}
	})
}
