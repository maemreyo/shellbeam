package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mediaMCPClient struct {
	catalog capability.Catalog
	media   media.Result
	last    bridge.Request
	bad     bool
}

func (c *mediaMCPClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	switch req.Action {
	case "inspect.server":
		catalog := c.catalog.Clone()
		return bridge.Response{Server: &catalog}, nil
	case "capabilities.negotiate":
		n, ok := capability.NegotiateMedia(*req.ConsumerMedia, capability.V1MediaSupport())
		if !ok {
			return bridge.Response{}, nil
		}
		return bridge.Response{NegotiatedMedia: &n}, nil
	case "read_media":
		result := c.media
		result.Data = append([]byte(nil), result.Data...)
		if c.bad {
			result.DisplayAddress.Path = "other.png"
		}
		return bridge.Response{Media: &result}, nil
	default:
		return bridge.Response{}, nil
	}
}

func mediaCatalog() capability.Catalog {
	catalog := capability.Baseline(capability.Limits{})
	catalog.Features[capability.FeatureRichLocalMedia] = capability.Available
	support := capability.V1MediaSupport()
	catalog.Media = &support
	return catalog
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mediaResult(t *testing.T, address media.DisplayAddress) media.Result {
	t.Helper()
	data := tinyPNG(t)
	return media.Result{
		SchemaVersion:  1,
		Kind:           "media",
		DisplayAddress: address,
		MIMEType:       "image/png",
		Format:         "png",
		ByteSize:       len(data),
		Width:          2,
		Height:         2,
		Data:           data,
	}
}

func TestMediaDiscoveryIsConditionalAndDisclosesOriginalByteEgress(t *testing.T) {
	without := capability.Baseline(capability.Limits{})
	withoutSession, closeWithout := currentSession(t, New(bridge.New(&mediaMCPClient{catalog: without}), without))
	defer closeWithout()
	withoutTools, err := withoutSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertReadMediaTransportAdvertisement(t, withoutTools.Tools, false)
	if strings.Contains(withoutTools.Tools[0].Description, "original selected local image file bytes") {
		t.Fatalf("media disclosure leaked into unavailable catalog: %q", withoutTools.Tools[0].Description)
	}

	client := &mediaMCPClient{catalog: capability.Baseline(capability.Limits{})}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	withSession, closeWith := currentSession(t, New(h, h.EffectiveCatalog()))
	defer closeWith()
	withTools, err := withSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertReadMediaTransportAdvertisement(t, withTools.Tools, true)
	description := withTools.Tools[0].Description
	for _, want := range []string{"original selected local image file bytes", "embedded metadata", "MCP client/model"} {
		if !strings.Contains(description, want) {
			t.Fatalf("missing disclosure %q in %q", want, description)
		}
	}
}

func TestReadMediaReturnsNativeImageAndSafeStructuredMetadata(t *testing.T) {
	address := media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp/../tmp", Path: "images/probe.png"}
	client := &mediaMCPClient{catalog: capability.Baseline(capability.Limits{}), media: mediaResult(t, address)}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	session, closeSession := currentSession(t, New(h, h.EffectiveCatalog()))
	defer closeSession()

	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"read_media","cwd":"/tmp/../tmp","path":"images/probe.png"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result=%#v", result)
	}
	var imageContent *mcpgo.ImageContent
	for _, content := range result.Content {
		if image, ok := content.(*mcpgo.ImageContent); ok {
			if imageContent != nil {
				t.Fatalf("multiple image content values: %#v", result.Content)
			}
			imageContent = image
		}
	}
	if imageContent == nil {
		t.Fatalf("missing native image content: %#v", result.Content)
	}
	if !bytes.Equal(imageContent.Data, client.media.Data) || imageContent.MIMEType != "image/png" || imageContent.Annotations != nil {
		t.Fatalf("image=%#v", imageContent)
	}
	if client.last.Media == nil || client.last.Media.CWD != address.CWD || client.last.Media.Path != address.Path {
		t.Fatalf("forwarded request normalized or missing: %#v", client.last)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"data"`, "base64", "token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("unsafe structured media metadata contains %q: %s", forbidden, encoded)
		}
	}
	for _, want := range []string{`"display_address"`, `"cwd":"/tmp/../tmp"`, `"path":"images/probe.png"`, `"mime_type":"image/png"`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("structured metadata missing %q: %s", want, encoded)
		}
	}
}

func TestReadMediaBridgeCorruptionReturnsErrorWithoutImage(t *testing.T) {
	address := media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: "ws_01K00000000000000000000000", Path: "probe.png"}
	client := &mediaMCPClient{catalog: capability.Baseline(capability.Limits{}), media: mediaResult(t, address), bad: true}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	session, closeSession := currentSession(t, New(h, h.EffectiveCatalog()))
	defer closeSession()
	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"read_media","workspace_id":"ws_01K00000000000000000000000","path":"probe.png"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected error: %#v", result)
	}
	for _, content := range result.Content {
		if _, ok := content.(*mcpgo.ImageContent); ok {
			t.Fatalf("image leaked on invalid daemon response: %#v", result.Content)
		}
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(encoded), "invalid_daemon_response") {
		t.Fatalf("wrong error mapping: %s", encoded)
	}
}

func TestMediaOutputSchemaIsConditionalAndContainsMetadataOnly(t *testing.T) {
	client := &mediaMCPClient{catalog: capability.Baseline(capability.Limits{})}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	session, closeSession := currentSession(t, New(h, h.EffectiveCatalog()))
	defer closeSession()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(tools.Tools[0].OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	props, _ := doc["properties"].(map[string]any)
	mediaProp, ok := props["media"].(map[string]any)
	if !ok {
		t.Fatalf("media output schema missing: %s", encoded)
	}
	mediaJSON, _ := json.Marshal(mediaProp)
	if strings.Contains(string(mediaJSON), `"data"`) || strings.Contains(string(mediaJSON), "base64") {
		t.Fatalf("media output schema exposes bytes: %s", mediaJSON)
	}
	action, _ := props["action"].(map[string]any)
	values, _ := action["enum"].([]any)
	found := false
	for _, value := range values {
		found = found || value == "read_media"
	}
	if !found {
		t.Fatalf("read_media missing from output action enum: %s", encoded)
	}
}

func TestReadMediaInvalidAddressBasesReturnNoImage(t *testing.T) {
	client := &mediaMCPClient{catalog: capability.Baseline(capability.Limits{})}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	session, closeSession := currentSession(t, New(h, h.EffectiveCatalog()))
	defer closeSession()
	cases := map[string]json.RawMessage{
		"missing-base": json.RawMessage(`{"action":"read_media","path":"probe.png"}`),
		"both-bases":   json.RawMessage(`{"action":"read_media","workspace_id":"ws_01K00000000000000000000000","cwd":"/tmp","path":"probe.png"}`),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			client.last = bridge.Request{}
			result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("expected invalid request: %#v", result)
			}
			for _, content := range result.Content {
				if _, ok := content.(*mcpgo.ImageContent); ok {
					t.Fatalf("invalid address emitted ImageContent: %#v", result.Content)
				}
			}
			if client.last.Action == "read_media" {
				t.Fatalf("invalid input reached daemon: %#v", client.last)
			}
		})
	}
}

func TestReadMediaCallGateRequiresEffectiveNegotiation(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	client := &mediaMCPClient{catalog: catalog, media: mediaResult(t, media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp", Path: "probe.png"})}
	session, closeSession := currentSession(t, New(bridge.New(client), catalog))
	defer closeSession()
	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"read_media","cwd":"/tmp","path":"probe.png"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	if !result.IsError || !strings.Contains(string(encoded), "feature_unavailable") {
		t.Fatalf("call gate result=%#v structured=%s", result, encoded)
	}
	if client.last.Action == "read_media" {
		t.Fatalf("unnegotiated media reached daemon: %#v", client.last)
	}
}

func assertReadMediaTransportAdvertisement(t *testing.T, tools []*mcpgo.Tool, want bool) {
	t.Helper()
	if len(tools) != 1 || tools[0].Name != "local_shell" {
		t.Fatalf("tools=%#v", tools)
	}
	encoded, err := json.Marshal(tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	if _, exists := doc["oneOf"]; exists {
		t.Fatalf("transport schema regained semantic oneOf validation: %s", encoded)
	}
	props, _ := doc["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	description, _ := action["description"].(string)
	got := strings.Contains(description, "read_media")
	if got != want {
		t.Fatalf("read_media advertised=%v want=%v action_description=%q schema=%s", got, want, description, encoded)
	}
}

type refreshableMCPMediaClient struct {
	catalog      capability.Catalog
	mediaEnabled bool
	media        media.Result
}

func (c *refreshableMCPMediaClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	switch req.Action {
	case "inspect.server":
		catalog := c.catalog.Clone()
		return bridge.Response{Server: &catalog}, nil
	case "capabilities.negotiate":
		if !c.mediaEnabled {
			return bridge.Response{Code: string(failure.FeatureUnavailable)}, nil
		}
		n, ok := capability.NegotiateMedia(*req.ConsumerMedia, capability.V1MediaSupport())
		if !ok {
			return bridge.Response{}, nil
		}
		return bridge.Response{NegotiatedMedia: &n}, nil
	case "read_media":
		result := c.media
		result.Data = append([]byte(nil), result.Data...)
		return bridge.Response{Media: &result}, nil
	default:
		return bridge.Response{}, nil
	}
}

func TestMCPRefreshesConditionalToolSchemaOnNextRequest(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{})
	client := &refreshableMCPMediaClient{catalog: catalog}
	h, err := bridge.NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	server := New(h, h.EffectiveCatalog())
	session, closeSession := currentSession(t, server)
	defer closeSession()

	before, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertReadMediaTransportAdvertisement(t, before.Tools, false)

	client.mediaEnabled = true
	client.media = mediaResult(t, media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp", Path: "refresh.png"})
	probe, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.server"}`)})
	if err != nil || probe.IsError {
		t.Fatalf("probe=%#v err=%v", probe, err)
	}

	after, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertReadMediaTransportAdvertisement(t, after.Tools, true)
	if !strings.Contains(after.Tools[0].Description, "original selected local image file bytes") {
		t.Fatalf("refreshed tool disclosure missing: %q", after.Tools[0].Description)
	}
	if !strings.Contains(after.Tools[0].Description, "inspect.project") || !strings.Contains(after.Tools[0].Description, "agent_bootstrap") {
		t.Fatalf("refreshed tool repository bootstrap pointer missing: %q", after.Tools[0].Description)
	}
}
