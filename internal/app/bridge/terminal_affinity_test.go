package bridge

import (
	"context"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestCaptureTerminalAffinityRequiresKnownAncestorNotRawTERMProgram(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	known := []core.TerminalIdentity{bridgeGhosttyIdentity()}
	got, err := CaptureTerminalAffinity(TerminalLaunchContext{ObservedAt: now, Environment: map[string]string{"TERM_PROGRAM": "ghostty"}, AncestorExecutables: []string{"/bin/fish"}}, known, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("raw TERM_PROGRAM became affinity: %+v", got)
	}
}

func TestCaptureTerminalAffinityMapsAncestryToConfiguredIdentityOnly(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	known := []core.TerminalIdentity{bridgeGhosttyIdentity()}
	got, err := CaptureTerminalAffinity(TerminalLaunchContext{ObservedAt: now, Environment: map[string]string{"TERM_PROGRAM": "ghostty"}, AncestorExecutables: []string{"/bin/fish", "/Applications/Ghostty.app/Contents/MacOS/ghostty"}}, known, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Identity != known[0] || got.Identity.ExecutableName != "ghostty" {
		t.Fatalf("affinity=%+v", got)
	}
}

func TestCaptureTerminalAffinityConflictingKnownEnvironmentFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	known := []core.TerminalIdentity{
		bridgeGhosttyIdentity(),
		{ProviderID: "wezterm", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.github.wez.wezterm", ExecutableName: "wezterm-gui"},
	}
	got, err := CaptureTerminalAffinity(TerminalLaunchContext{ObservedAt: now, Environment: map[string]string{"TERM_PROGRAM": "wezterm"}, AncestorExecutables: []string{"/Applications/Ghostty.app/Contents/MacOS/ghostty"}}, known, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("conflicting evidence selected=%+v", got)
	}
}

func TestHandlerStoresOnlyTypedMemoryAffinity(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	hint, _ := core.NewBridgeAffinityHint(bridgeGhosttyIdentity(), now, time.Minute)
	h := New(noopAffinityClient{})
	if err := h.SetTerminalAffinity(hint); err != nil {
		t.Fatal(err)
	}
	got := h.TerminalAffinity()
	if got == nil || *got != hint {
		t.Fatalf("stored=%+v", got)
	}
	copy := *got
	copy.Identity.ProviderID = "changed"
	if h.TerminalAffinity().Identity.ProviderID != "ghostty" {
		t.Fatal("handler aliases caller mutation")
	}
}

type noopAffinityClient struct{}

func (noopAffinityClient) Forward(context.Context, Request) (Response, error) { return Response{}, nil }

func bridgeGhosttyIdentity() core.TerminalIdentity {
	return core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
}

type affinityCaptureClient struct{ got Request }

func (c *affinityCaptureClient) Forward(_ context.Context, req Request) (Response, error) {
	c.got = req
	return Response{}, nil
}

func TestHandlerInjectsBridgeAffinityOnlyIntoHandoffRequest(t *testing.T) {
	now := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC)
	hint, err := core.NewBridgeAffinityHint(bridgeGhosttyIdentity(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := &affinityCaptureClient{}
	h := New(client)
	if err := h.SetTerminalAffinity(hint); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(t.Context(), Request{ProtocolVersion: 2, Action: "handoff.request"}); err != nil {
		t.Fatal(err)
	}
	if client.got.TerminalAffinity == nil || *client.got.TerminalAffinity != hint {
		t.Fatalf("forwarded hint=%#v", client.got.TerminalAffinity)
	}
	if _, err := h.Handle(t.Context(), Request{ProtocolVersion: 2, Action: "inspect.server"}); err != nil {
		t.Fatal(err)
	}
	if client.got.TerminalAffinity != nil {
		t.Fatalf("non-handoff request leaked terminal affinity: %#v", client.got.TerminalAffinity)
	}
}
