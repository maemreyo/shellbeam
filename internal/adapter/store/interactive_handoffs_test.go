package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestHandoffReserveReplaysExactRequestAndRotatesDelegatedAuthority(t *testing.T) {
	r, binding, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "reserve")
	stored, created, result := r.ReserveHandoff(context.Background(), req, initial)
	if result.Err != nil || !created || stored != initial {
		t.Fatalf("reserve stored=%#v created=%v result=%#v", stored, created, result)
	}
	rotated, err := r.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || rotated.AuthorityEpoch != initial.AuthorityEpoch || rotated.DesiredOwner != delegated.OwnerHuman || rotated.Lifecycle != delegated.LifecycleLive {
		t.Fatalf("rotated binding=%#v err=%v", rotated, err)
	}
	loadedReq, loaded, err := r.LoadHandoff(context.Background(), req.HandoffID)
	if err != nil || loadedReq != req || loaded != initial {
		t.Fatalf("loaded req=%#v state=%#v err=%v", loadedReq, loaded, err)
	}
	replay, created, result := r.ReserveHandoff(context.Background(), req, initial)
	if result.Err != nil || created || replay != initial {
		t.Fatalf("replay=%#v created=%v result=%#v", replay, created, result)
	}
	changed := req
	changed.Reason = handoff.ReasonHumanConfirmation
	if _, _, result := r.ReserveHandoff(context.Background(), changed, initial); !errors.Is(result.Err, failure.HandoffConflict) {
		t.Fatalf("changed request err=%v", result.Err)
	}
}

func TestHandoffReserveRequiresLiveCurrentDelegatedAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 32)
	res := task4DelegatedReservation("op-handoff-not-live", "session-handoff-not-live", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_not_live")
	if _, _, result := r.ReserveDelegatedBinding(context.Background(), binding, ref); result.Err != nil {
		t.Fatal(result.Err)
	}
	req, initial := h2HandoffRequestAndState(binding, "not-live")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err == nil {
		t.Fatal("handoff accepted while delegated binding was not live")
	}
	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if result := r.AdvanceDelegatedBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	stale := initial
	stale.AuthorityEpoch = live.AuthorityEpoch + 2
	if _, _, result := r.ReserveHandoff(context.Background(), req, stale); !errors.Is(result.Err, failure.StaleControlGeneration) {
		t.Fatalf("future epoch err=%v", result.Err)
	}
}

func h2HandoffFixture(t *testing.T, root, suffix string) (*Repository, delegated.Binding, handoff.Request, handoff.State) {
	t.Helper()
	r := delegatedRepository(t, root, 64)
	res := task4DelegatedReservation("op-handoff-"+suffix, "session-handoff-"+suffix, "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_"+suffix)
	if _, _, result := r.ReserveDelegatedBinding(context.Background(), binding, ref); result.Err != nil {
		t.Fatal(result.Err)
	}
	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if result := r.AdvanceDelegatedBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}
	req, initial := h2HandoffRequestAndState(live, suffix)
	return r, live, req, initial
}

func h2HandoffRequestAndState(binding delegated.Binding, suffix string) (handoff.Request, handoff.State) {
	req := handoff.Request{
		HandoffID:  "handoff-" + suffix,
		SessionID:  binding.SessionID,
		Reason:     handoff.ReasonManualIntervention,
		Privacy:    handoff.PrivacyStandard,
		Completion: handoff.Completion{Kind: handoff.CompletionManualReady},
	}
	state := handoff.State{
		SchemaVersion: handoff.StateSchemaVersion,
		HandoffID:     req.HandoffID, SessionID: req.SessionID, Phase: handoff.PhaseAgentFencing,
		AuthorityEpoch: binding.AuthorityEpoch + 1, DesiredOwner: delegated.OwnerHuman, ProviderOwner: delegated.OwnerAgent,
		AgentIngress: handoff.IngressUnknown, HumanIngress: handoff.IngressFenced,
		TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryNone},
		PrivacyState:     handoff.PrivacyStateStandard, PrivacyRelease: handoff.PrivacyReleaseNotRequired, CaptureState: handoff.CapturePublic,
		ProviderGeneration: "provider-generation-" + suffix,
	}
	return req, state
}
