package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type publicHandoffMCPClient struct {
	last  bridge.Request
	calls int
	state handoff.PublicState
}

func (c *publicHandoffMCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.calls++
	c.last = req
	return bridge.Response{Handoff: &c.state}, nil
}

func TestInteractiveHandoffMCPV2UsesOneToolAndLegacyRemainsClosed(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	created := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Second)
	client := &publicHandoffMCPClient{state: handoff.PublicState{SchemaVersion: 1, HandoffID: "handoff_public_1", SessionID: "session_public_1", AuthorityEpoch: 4, Status: handoff.StatusHumanConnecting, AgentIngress: handoff.IngressFenced, HumanIngress: handoff.IngressFenced, TransferBoundary: handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}, CreatedAt: &created, UpdatedAt: &updated, AttachArgv: []string{"shellbeam", "session", "attach", "--handoff-id", "handoff_public_1"}}}
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"manual_intervention","privacy":"standard","completion":{"kind":"manual_ready"}}`)})
	if err != nil || res.IsError || client.calls != 1 || client.last.Action != "handoff.request" || client.last.HandoffRequest.HandoffID != "handoff_public_1" {
		t.Fatalf("result=%#v err=%v calls=%d request=%#v", res, err, client.calls, client.last)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["handoff"] == nil {
		t.Fatalf("handoff body=%#v", res.StructuredContent)
	}
	wire, _ := json.Marshal(body)
	for _, forbidden := range []string{"provider_generation", "human_client", "client_ref", "pane_id", "window_id"} {
		if strings.Contains(string(wire), forbidden) {
			t.Fatalf("unsafe MCP handoff projection contains %q: %s", forbidden, wire)
		}
	}

	legacyRaw := []byte(`{"action":"handoff.request","handoff_id":"handoff_public_1","session_id":"session_public_1","reason":"manual_intervention","privacy":"standard","completion":{"kind":"manual_ready"}}`)
	in, err := decodeInputV2(legacyRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(1, in, legacyRaw); err == nil {
		t.Fatal("legacy MCP accepted interactive handoff action")
	}
}

func TestInteractiveHandoffMCPV2MapsWaitAbortInspect(t *testing.T) {
	for _, raw := range []string{
		`{"action":"handoff.wait","handoff_id":"handoff_public_1","yield_time_ms":1000}`,
		`{"action":"handoff.abort","handoff_id":"handoff_public_1"}`,
		`{"action":"inspect.handoff","handoff_id":"handoff_public_1"}`,
	} {
		data := []byte(raw)
		in, err := decodeInputV2(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateForVersion(2, in, data); err != nil {
			t.Fatalf("validate %s: %v", raw, err)
		}
		req := requestFromInput(2, in, data)
		if req.Action != in.Action {
			t.Fatalf("mapping %s -> %#v", raw, req)
		}
		switch in.Action {
		case "handoff.wait":
			if req.HandoffWait.HandoffID != "handoff_public_1" || req.HandoffWait.Yield != time.Second {
				t.Fatalf("wait mapping %s -> %#v", raw, req.HandoffWait)
			}
		default:
			if req.HandoffID != "handoff_public_1" {
				t.Fatalf("mapping %s -> %#v", raw, req)
			}
		}
	}
}

func TestInteractiveHandoffCatalogDoesNotClaimSecretOrAutomaticReadiness(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	if catalog.InteractiveHandoff == nil || !catalog.InteractiveHandoff.ManualStandard || catalog.InteractiveHandoff.Secret || catalog.InteractiveHandoff.AutomaticReadiness {
		t.Fatalf("catalog=%#v", catalog.InteractiveHandoff)
	}
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.Features[capability.FeatureDelegatedInteractive] != capability.Available {
		t.Fatalf("features=%#v", catalog.Features)
	}
}
