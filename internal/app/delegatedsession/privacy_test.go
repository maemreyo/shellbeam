package delegatedsession

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type privacyCapableProvider struct {
	countingProvider
	armed, proved, released int
	handle                  PrivacyHandle
	proof                   PrivateObservationProof
}

func (p *privacyCapableProvider) ArmPrivateObservation(context.Context, core.ProviderRef, PrivacySpec) (PrivacyHandle, error) {
	p.armed++
	return p.handle, nil
}
func (p *privacyCapableProvider) ProvePrivateObservation(context.Context, core.ProviderRef, PrivacyHandle) (PrivateObservationProof, error) {
	p.proved++
	return p.proof, nil
}
func (p *privacyCapableProvider) ReleasePrivateObservation(context.Context, core.ProviderRef, PrivacyHandle, ForwardBoundary) error {
	p.released++
	return nil
}

func TestPrivacyProviderServiceForwardsTypedOperations(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	handle := PrivacyHandle{OpaqueRef: "privacy_ref_1", Generation: "gen_1"}
	proof := PrivateObservationProof{Handle: handle, ProviderGeneration: "provider_gen_1", PrivateFromFirstByte: true, ObservedAt: now}
	p := &privacyCapableProvider{handle: handle, proof: proof}
	svc := New(p)
	ref := core.ProviderRef{Ref: "provider_ref_1"}
	spec := PrivacySpec{HandoffID: "handoff-1", AuthorityEpoch: 4}

	got, err := svc.ArmPrivateObservation(t.Context(), ref, spec)
	if err != nil || got != handle {
		t.Fatalf("arm=%#v err=%v", got, err)
	}
	gotProof, err := svc.ProvePrivateObservation(t.Context(), ref, handle)
	if err != nil || gotProof != proof {
		t.Fatalf("proof=%#v err=%v", gotProof, err)
	}
	boundary := ForwardBoundary{Proof: shell.PrivacyReleaseProof{
		HandoffID: "handoff-1", AuthorityEpoch: 4,
		Shell:    shell.ShellIdentity{Family: shell.ShellFish, RuntimeID: "runtime-1"},
		Boundary: "prompt-boundary-1", ForwardOnly: true, ObservedAt: now,
	}}
	if err := svc.ReleasePrivateObservation(t.Context(), ref, handle, boundary); err != nil {
		t.Fatal(err)
	}
	if p.armed != 1 || p.proved != 1 || p.released != 1 {
		t.Fatalf("counts=%#v", p)
	}
}

func TestPrivacyProviderServiceFailsClosedWhenCapabilityMissing(t *testing.T) {
	svc := New(&countingProvider{})
	_, err := svc.ArmPrivateObservation(t.Context(), core.ProviderRef{Ref: "provider_ref_1"}, PrivacySpec{HandoffID: "handoff-1", AuthorityEpoch: 2})
	if !errors.Is(err, failure.PrivateOutputBarrierFailed) {
		t.Fatalf("err=%v want private_output_barrier_failed", err)
	}
}

func TestPrivacyTypesRejectStaleOrUnprovenBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	spec := PrivacySpec{HandoffID: "handoff-1", AuthorityEpoch: 4}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	handle := PrivacyHandle{OpaqueRef: "privacy_ref_1", Generation: "gen_1"}
	if err := handle.Validate(); err != nil {
		t.Fatal(err)
	}
	proof := PrivateObservationProof{Handle: handle, ProviderGeneration: "provider_gen_1", PrivateFromFirstByte: true, ObservedAt: now}
	if err := proof.Validate(); err != nil {
		t.Fatal(err)
	}

	valid := ForwardBoundary{Proof: shell.PrivacyReleaseProof{
		HandoffID: spec.HandoffID, AuthorityEpoch: spec.AuthorityEpoch,
		Shell:    shell.ShellIdentity{Family: shell.ShellBash, RuntimeID: "runtime-1"},
		Boundary: "boundary-1", ForwardOnly: true, ObservedAt: now,
	}}
	if err := valid.ValidateFor(spec); err != nil {
		t.Fatalf("valid boundary rejected: %v", err)
	}
	stale := valid
	stale.Proof.AuthorityEpoch = 3
	if err := stale.ValidateFor(spec); err == nil {
		t.Fatal("stale privacy release accepted")
	}
	backward := valid
	backward.Proof.ForwardOnly = false
	if err := backward.ValidateFor(spec); err == nil {
		t.Fatal("non-forward privacy release accepted")
	}
}
