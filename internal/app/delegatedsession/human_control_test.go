package delegatedsession

import (
	"bytes"
	"context"
	"errors"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type humanCapableProvider struct {
	countingProvider
	attached, writable, fenced, inspected, armed, waited, detached int
}

func (p *humanCapableProvider) AttachHuman(context.Context, core.ProviderRef, HumanAttachSpec) (HumanAttachResult, error) {
	p.attached++
	return HumanAttachResult{ClientRef: ProviderClientRef{Ref: "client_ref_1"}, ObservedOwner: core.OwnerAgent}, nil
}
func (p *humanCapableProvider) SetHumanWritable(context.Context, core.ProviderRef, ProviderClientRef, bool) error {
	p.writable++
	return nil
}
func (p *humanCapableProvider) FenceHumanIngress(context.Context, core.ProviderRef, ProviderClientRef, core.AuthorityEpoch) (IngressFenceProof, error) {
	p.fenced++
	return IngressFenceProof{Fenced: true, AuthorityEpoch: 2}, nil
}
func (p *humanCapableProvider) InspectHumanClient(context.Context, core.ProviderRef, ProviderClientRef) (HumanClientObservation, error) {
	p.inspected++
	return HumanClientObservation{Present: true, ReadOnly: true}, nil
}
func (p *humanCapableProvider) ArmWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) error {
	p.armed++
	return nil
}
func (p *humanCapableProvider) WaitWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) (handoff.HumanControlKind, error) {
	p.waited++
	return handoff.HumanControlReady, nil
}
func (p *humanCapableProvider) PrepareReadOnlyLocalControl(context.Context, core.ProviderRef, ProviderClientRef) error {
	p.detached++
	return nil
}

func TestHumanProviderServiceForwardsQualifiedOperations(t *testing.T) {
	p := &humanCapableProvider{}
	svc := New(p)
	ref := core.ProviderRef{Ref: "provider_ref_1"}
	client := ProviderClientRef{Ref: "client_ref_1"}
	spec := HumanAttachSpec{Stdin: bytes.NewBuffer(nil), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Environment: []string{"TERM=xterm-256color"}}
	result, err := svc.AttachHuman(context.Background(), ref, spec)
	if err != nil || result.ClientRef != client {
		t.Fatalf("attach=%#v err=%v", result, err)
	}
	if err := svc.SetHumanWritable(context.Background(), ref, client, true); err != nil {
		t.Fatal(err)
	}
	proof, err := svc.FenceHumanIngress(context.Background(), ref, client, 2)
	if err != nil || !proof.Fenced || proof.AuthorityEpoch != 2 {
		t.Fatalf("proof=%#v err=%v", proof, err)
	}
	if _, err := svc.InspectHumanClient(context.Background(), ref, client); err != nil {
		t.Fatal(err)
	}
	control := HumanControlSpec{HandoffID: "handoff-1", AuthorityEpoch: 2}
	if err := svc.ArmWritableHumanControl(context.Background(), ref, client, control); err != nil {
		t.Fatal(err)
	}
	kind, err := svc.WaitWritableHumanControl(context.Background(), ref, client, control)
	if err != nil || kind != handoff.HumanControlReady {
		t.Fatalf("kind=%q err=%v", kind, err)
	}
	if err := svc.PrepareReadOnlyLocalControl(context.Background(), ref, client); err != nil {
		t.Fatal(err)
	}
	if p.attached != 1 || p.writable != 1 || p.fenced != 1 || p.inspected != 1 || p.armed != 1 || p.waited != 1 || p.detached != 1 {
		t.Fatalf("counts=%#v", p)
	}
}

func TestHumanProviderServiceFailsClosedWhenCapabilityMissing(t *testing.T) {
	svc := New(&countingProvider{})
	_, err := svc.AttachHuman(context.Background(), core.ProviderRef{Ref: "provider_ref_1"}, HumanAttachSpec{})
	if err == nil {
		t.Fatal("human attach accepted without human provider capability")
	}
	var typed interface{ Error() string }
	if !errors.As(err, &typed) {
		t.Fatalf("untyped error: %T %v", err, err)
	}
}
