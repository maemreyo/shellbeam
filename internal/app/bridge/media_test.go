package bridge

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
)

type scriptedClient struct {
	requests  []Request
	responses []Response
	errs      []error
}

func (c *scriptedClient) Forward(_ context.Context, req Request) (Response, error) {
	c.requests = append(c.requests, req)
	i := len(c.requests) - 1
	var out Response
	if i < len(c.responses) {
		out = c.responses[i]
	}
	var err error
	if i < len(c.errs) {
		err = c.errs[i]
	}
	return out, err
}

func baselineCatalog() capability.Catalog {
	return capability.Baseline(capability.Limits{CommandBytes: 123})
}

func negotiatedResponse(t *testing.T, consumer, daemon capability.MediaSupport) Response {
	t.Helper()
	n, ok := capability.NegotiateMedia(consumer, daemon)
	if !ok {
		t.Fatal("negotiate fixture")
	}
	return Response{NegotiatedMedia: &n}
}

func TestNewNegotiatedBuildsEffectiveCatalogAndInterceptsInspectServer(t *testing.T) {
	consumer := capability.V1MediaSupport()
	daemon := capability.V1MediaSupport()
	daemon.MaxImageBytes = 5 << 20
	base := baselineCatalog()
	client := &scriptedClient{responses: []Response{{Server: &base}, negotiatedResponse(t, consumer, daemon)}}
	h, err := NewNegotiated(context.Background(), client, consumer)
	if err != nil {
		t.Fatal(err)
	}
	eff := h.EffectiveCatalog()
	if eff.Features[capability.FeatureRichLocalMedia] != capability.Available || eff.Media == nil || eff.Media.MaxImageBytes != 5<<20 {
		t.Fatalf("effective=%#v", eff)
	}
	// Clone isolation.
	eff.Media.MIMETypes[0] = "changed"
	if h.EffectiveCatalog().Media.MIMETypes[0] == "changed" {
		t.Fatal("effective catalog aliases caller")
	}
	before := len(client.requests)
	got, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "inspect.server"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != before || got.Server == nil || got.Server.Media == nil || got.Server.Media.MaxImageBytes != 5<<20 {
		t.Fatalf("requests=%d response=%#v", len(client.requests), got)
	}
}

func TestNewNegotiatedOldDaemonDegradesMediaUnavailable(t *testing.T) {
	base := baselineCatalog()
	client := &scriptedClient{responses: []Response{{Server: &base}, {Code: string(failure.FeatureUnavailable)}}}
	h, err := NewNegotiated(context.Background(), client, capability.V1MediaSupport())
	if err != nil {
		t.Fatal(err)
	}
	eff := h.EffectiveCatalog()
	if eff.Media != nil || eff.Features[capability.FeatureRichLocalMedia] != capability.Unavailable {
		t.Fatalf("effective=%#v", eff)
	}
}

func TestNewNegotiatedRejectsMalformedNegotiationButNewPreservesNoMediaCompatibility(t *testing.T) {
	base := baselineCatalog()
	client := &scriptedClient{responses: []Response{{Server: &base}, {}}}
	if _, err := NewNegotiated(context.Background(), client, capability.V1MediaSupport()); !errors.Is(err, failure.InvalidDaemonResponse) {
		t.Fatalf("err=%v", err)
	}
	legacy := New(&scriptedClient{})
	if legacy.EffectiveCatalog().Media != nil {
		t.Fatal("legacy New advertised media")
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func validBridgeMedia(t *testing.T, address media.DisplayAddress) media.Result {
	data := pngBytes(t, 2, 2)
	return media.Result{SchemaVersion: 1, Kind: "media", DisplayAddress: address, MIMEType: "image/png", Format: "png", ByteSize: len(data), Width: 2, Height: 2, Data: data}
}

func negotiatedHandlerForMedia(t *testing.T, result media.Result) (*Handler, *scriptedClient) {
	t.Helper()
	consumer := capability.V1MediaSupport()
	base := baselineCatalog()
	n := negotiatedResponse(t, consumer, capability.V1MediaSupport())
	client := &scriptedClient{responses: []Response{{Server: &base}, n, {Media: &result}}}
	h, err := NewNegotiated(context.Background(), client, consumer)
	if err != nil {
		t.Fatal(err)
	}
	return h, client
}

func TestReadMediaDerivesExactExpectedAddressAndInjectsNegotiation(t *testing.T) {
	expected := media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp/../tmp", Path: "a.png"}
	h, client := negotiatedHandlerForMedia(t, validBridgeMedia(t, expected))
	got, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "read_media", Media: &daemonapp.MediaRequest{CWD: expected.CWD, Path: expected.Path}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Media == nil || got.Media.DisplayAddress != expected {
		t.Fatalf("media=%#v", got.Media)
	}
	req := client.requests[len(client.requests)-1]
	if req.ConsumerMedia == nil || req.MediaContractFingerprint == "" || req.Media == nil || req.Media.CWD != expected.CWD {
		t.Fatalf("forwarded=%#v", req)
	}
}

func TestReadMediaRejectsDaemonAddressAndMetadataCorruption(t *testing.T) {
	workspace := "ws_01K00000000000000000000000"
	expected := media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: workspace, Path: "artifacts/a.png"}
	base := validBridgeMedia(t, expected)
	cases := map[string]func(*media.Result){
		"wrong-workspace": func(r *media.Result) { r.DisplayAddress.WorkspaceID = "ws_01K00000000000000000000001" },
		"wrong-kind": func(r *media.Result) {
			r.DisplayAddress.AddressKind = media.AddressCWD
			r.DisplayAddress.WorkspaceID = ""
			r.DisplayAddress.CWD = "/tmp"
		},
		"normalized-path": func(r *media.Result) { r.DisplayAddress.Path = "artifacts/./a.png" },
		"byte-size":       func(r *media.Result) { r.ByteSize++ },
		"mime":            func(r *media.Result) { r.MIMEType = "image/jpeg" },
		"format":          func(r *media.Result) { r.Format = "jpeg" },
		"width":           func(r *media.Result) { r.Width++ },
		"invalid-bytes":   func(r *media.Result) { r.Data = []byte("not png"); r.ByteSize = len(r.Data) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := base
			bad.Data = append([]byte(nil), base.Data...)
			mutate(&bad)
			h, _ := negotiatedHandlerForMedia(t, bad)
			got, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "read_media", Media: &daemonapp.MediaRequest{WorkspaceID: workspace, Path: expected.Path}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Code != string(failure.InvalidDaemonResponse) {
				t.Fatalf("code=%q response=%#v", got.Code, got)
			}
			if got.Media != nil {
				t.Fatalf("media leaked=%#v", got.Media)
			}
		})
	}
}

func TestNewNegotiatedRejectsTamperedFingerprint(t *testing.T) {
	consumer := capability.V1MediaSupport()
	base := baselineCatalog()
	response := negotiatedResponse(t, consumer, capability.V1MediaSupport())
	response.NegotiatedMedia.Fingerprint = strings.Repeat("0", 64)
	client := &scriptedClient{responses: []Response{{Server: &base}, response}}
	if _, err := NewNegotiated(context.Background(), client, consumer); !errors.Is(err, failure.InvalidDaemonResponse) {
		t.Fatalf("err=%v", err)
	}
}

func TestReadMediaRejectsCanonicalCWDSubstitution(t *testing.T) {
	expected := media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp/../tmp", Path: "a.png"}
	bad := validBridgeMedia(t, expected)
	bad.DisplayAddress.CWD = "/tmp"
	h, _ := negotiatedHandlerForMedia(t, bad)
	got, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "read_media", Media: &daemonapp.MediaRequest{CWD: expected.CWD, Path: expected.Path}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != string(failure.InvalidDaemonResponse) || got.Media != nil {
		t.Fatalf("response=%#v", got)
	}
}
