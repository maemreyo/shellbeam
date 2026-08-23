package delegatedsession

import (
	"context"
	"errors"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type countingProvider struct{ probes, creates, reattaches, writes, signals, inspects, closes int }

func (*countingProvider) Identity() core.ProviderIdentity {
	return core.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
}
func (*countingProvider) ProviderRefForSession(sessionID string, at time.Time) (core.ProviderRef, error) {
	return core.ProviderRef{SchemaVersion: core.ProviderRefSchemaVersion, SessionID: sessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "ref_" + sessionID, CreatedAt: at, UpdatedAt: at}, nil
}
func (p *countingProvider) Probe(context.Context) error { p.probes++; return nil }
func (p *countingProvider) Create(context.Context, CreateRequest) (CreateResult, error) {
	p.creates++
	return CreateResult{}, nil
}
func (p *countingProvider) Reattach(context.Context, core.ProviderRef, OutputSink) (Observation, error) {
	p.reattaches++
	return Observation{}, nil
}
func (p *countingProvider) Write(context.Context, core.ProviderRef, []byte) error {
	p.writes++
	return nil
}
func (p *countingProvider) Signal(context.Context, core.ProviderRef, string) error {
	p.signals++
	return nil
}
func (p *countingProvider) Inspect(context.Context, core.ProviderRef) (Observation, error) {
	p.inspects++
	return Observation{}, nil
}
func (p *countingProvider) Wait(context.Context, core.ProviderRef) (Observation, error) {
	return Observation{}, nil
}
func (p *countingProvider) Close(context.Context, core.ProviderRef) error { p.closes++; return nil }

func TestNonDelegatedStartNeverTouchesProvider(t *testing.T) {
	p := &countingProvider{}
	svc := New(p)
	for _, mode := range []string{"", "direct", "persistent"} {
		_, handled, err := svc.Start(context.Background(), mode, CreateRequest{})
		if err != nil || handled {
			t.Fatalf("mode=%q handled=%v err=%v", mode, handled, err)
		}
	}
	if *p != (countingProvider{}) {
		t.Fatalf("provider touched by non-delegated start: %#v", p)
	}
}

func TestDelegatedStartProbesThenCreates(t *testing.T) {
	p := &countingProvider{}
	svc := New(p)
	_, handled, err := svc.Start(context.Background(), core.ModeDelegatedInteractive, CreateRequest{})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if p.probes != 1 || p.creates != 1 {
		t.Fatalf("provider counts=%#v", p)
	}
}

func TestDelegatedStartStopsWhenProbeFails(t *testing.T) {
	sentinel := errors.New("unqualified")
	p := &failingProbeProvider{err: sentinel}
	svc := New(p)
	_, handled, err := svc.Start(context.Background(), core.ModeDelegatedInteractive, CreateRequest{})
	if !handled || !errors.Is(err, sentinel) || p.creates != 0 {
		t.Fatalf("handled=%v err=%v creates=%d", handled, err, p.creates)
	}
}

type failingProbeProvider struct {
	err     error
	creates int
}

func (*failingProbeProvider) Identity() core.ProviderIdentity {
	return core.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
}
func (*failingProbeProvider) ProviderRefForSession(sessionID string, at time.Time) (core.ProviderRef, error) {
	return core.ProviderRef{SchemaVersion: core.ProviderRefSchemaVersion, SessionID: sessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "ref_" + sessionID, CreatedAt: at, UpdatedAt: at}, nil
}
func (p *failingProbeProvider) Probe(context.Context) error { return p.err }
func (p *failingProbeProvider) Create(context.Context, CreateRequest) (CreateResult, error) {
	p.creates++
	return CreateResult{}, nil
}
func (*failingProbeProvider) Reattach(context.Context, core.ProviderRef, OutputSink) (Observation, error) {
	return Observation{}, nil
}
func (*failingProbeProvider) Write(context.Context, core.ProviderRef, []byte) error  { return nil }
func (*failingProbeProvider) Signal(context.Context, core.ProviderRef, string) error { return nil }
func (*failingProbeProvider) Inspect(context.Context, core.ProviderRef) (Observation, error) {
	return Observation{}, nil
}
func (*failingProbeProvider) Wait(context.Context, core.ProviderRef) (Observation, error) {
	return Observation{}, nil
}
func (*failingProbeProvider) Close(context.Context, core.ProviderRef) error { return nil }
