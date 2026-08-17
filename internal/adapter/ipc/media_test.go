//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
)

type mediaActionsFake struct {
	fakeActions
	reads   atomic.Int32
	result  media.Result
	err     error
	support capability.MediaSupport
}

func (f *mediaActionsFake) ReadMedia(context.Context, daemonapp.MediaRequest) (media.Result, error) {
	f.reads.Add(1)
	return f.result, f.err
}
func (f *mediaActionsFake) MediaSupport() capability.MediaSupport {
	if f.support.SchemaVersion == 0 {
		return capability.V1MediaSupport()
	}
	return f.support
}

func TestMediaV2NegotiationAndReadAreStateless(t *testing.T) {
	actions := &mediaActionsFake{result: media.Result{SchemaVersion: 1, Kind: "media", DisplayAddress: media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp", Path: "a.png"}, MIMEType: "image/png", Format: "png", ByteSize: 3, Width: 1, Height: 1, Data: []byte{1, 2, 3}}}
	consumer := capability.V1MediaSupport()
	negReq := RequestV2{IPVersion: 2, Kind: "request", RequestID: "n", Action: "capabilities.negotiate", ConsumerMedia: &consumer}
	var negResp ResponseV2
	if err := dispatchMediaV2(context.Background(), negReq, &negResp, actions); err != nil {
		t.Fatal(err)
	}
	if negResp.NegotiatedMedia == nil || negResp.NegotiatedMedia.Fingerprint == "" {
		t.Fatalf("negotiated=%#v", negResp.NegotiatedMedia)
	}

	readReq := RequestV2{IPVersion: 2, Kind: "request", RequestID: "r", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: negResp.NegotiatedMedia.Fingerprint, Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}}
	var readResp ResponseV2
	if err := dispatchMediaV2(context.Background(), readReq, &readResp, actions); err != nil {
		t.Fatal(err)
	}
	if actions.reads.Load() != 1 || readResp.Media == nil || !bytes.Equal(readResp.Media.Data, []byte{1, 2, 3}) {
		t.Fatalf("reads=%d media=%#v", actions.reads.Load(), readResp.Media)
	}
}

func TestMediaV2RejectsMissingOptInOrWrongFingerprintBeforeRead(t *testing.T) {
	actions := &mediaActionsFake{}
	consumer := capability.V1MediaSupport()
	cases := []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "a", Action: "read_media", Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}},
		{IPVersion: 2, Kind: "request", RequestID: "b", Action: "read_media", ConsumerMedia: &consumer, Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}},
		{IPVersion: 2, Kind: "request", RequestID: "c", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: "deadbeef", Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}},
	}
	for _, req := range cases {
		var resp ResponseV2
		if err := dispatchMediaV2(context.Background(), req, &resp, actions); !errors.Is(err, failure.FeatureUnavailable) {
			t.Fatalf("req=%s err=%v", req.RequestID, err)
		}
	}
	if actions.reads.Load() != 0 {
		t.Fatalf("read called %d times", actions.reads.Load())
	}
}

func TestMediaV2RequestDecodeFieldSets(t *testing.T) {
	consumer := `{"schema_version":1,"kinds":["image"],"mime_types":["image/png"],"max_image_bytes":7340032,"max_width":16384,"max_height":16384,"max_pixels":40000000}`
	valid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"n","action":"capabilities.negotiate","consumer_media":` + consumer + `}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"read_media","consumer_media":` + consumer + `,"media_contract_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media":{"cwd":"/tmp","path":"a.png"}}`,
	}
	for _, raw := range valid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err != nil {
			t.Fatalf("valid %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"n","action":"capabilities.negotiate"}`,
		`{"ipc_version":2,"kind":"request","request_id":"n","action":"capabilities.negotiate","consumer_media":` + consumer + `,"media":{"cwd":"/tmp","path":"a.png"}}`,
		`{"ipc_version":2,"kind":"request","request_id":"r","action":"read_media","consumer_media":` + consumer + `,"media_contract_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	}
	for _, raw := range invalid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err == nil {
			t.Fatalf("invalid accepted %s", raw)
		}
	}
}

type errorAfterReader struct {
	data []byte
	sent bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("injected transport read failure")
}
func (r *errorAfterReader) Close() error { return nil }

func mediaHTTPClient(body io.ReadCloser) *Client {
	return &Client{http: &http.Client{Transport: roundTripV2Func(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})}}
}

func validMediaResponseJSON(req RequestV2) []byte {
	return []byte(`{"ipc_version":2,"kind":"response","request_id":"` + req.RequestID + `","action":"read_media","ok":true,"media":{"schema_version":1,"kind":"media","display_address":{"address_kind":"cwd","cwd":"/tmp","path":"a.png"},"mime_type":"image/png","format":"png","byte_size":3,"width":1,"height":1,"data":"AQID"}}`)
}

func TestMediaV2ClientRejectsReadErrorBeforeDecodingValidPrefix(t *testing.T) {
	consumer := capability.V1MediaSupport()
	req := RequestV2{IPVersion: 2, Kind: "request", RequestID: "r", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: strings.Repeat("a", 64), Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}}
	out, err := mediaHTTPClient(&errorAfterReader{data: validMediaResponseJSON(req)}).CallV2(context.Background(), req)
	if !errors.Is(err, failure.InvalidDaemonResponse) {
		t.Fatalf("err=%v", err)
	}
	if out.Media != nil {
		t.Fatalf("media decoded despite read error: %#v", out.Media)
	}
}

func TestMediaV2ClientRejectsOversizeMalformedBase64AndEnvelopeMismatch(t *testing.T) {
	consumer := capability.V1MediaSupport()
	req := RequestV2{IPVersion: 2, Kind: "request", RequestID: "r", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: strings.Repeat("a", 64), Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}}
	valid := validMediaResponseJSON(req)
	cases := map[string][]byte{
		"oversize":       bytes.Repeat([]byte{'x'}, media.MaxOuterResponseBytes+1),
		"short-response": append([]byte(nil), valid[:len(valid)-7]...),
		"bad-base64":     []byte(`{"ipc_version":2,"kind":"response","request_id":"r","action":"read_media","ok":true,"media":{"schema_version":1,"kind":"media","display_address":{"address_kind":"cwd","cwd":"/tmp","path":"a.png"},"mime_type":"image/png","format":"png","byte_size":3,"width":1,"height":1,"data":"***"}}`),
		"wrong-request":  bytes.Replace(validMediaResponseJSON(req), []byte(`"request_id":"r"`), []byte(`"request_id":"other"`), 1),
		"wrong-action":   bytes.Replace(validMediaResponseJSON(req), []byte(`"action":"read_media"`), []byte(`"action":"poll"`), 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := mediaHTTPClient(io.NopCloser(bytes.NewReader(body))).CallV2(context.Background(), req)
			if !errors.Is(err, failure.InvalidDaemonResponse) {
				t.Fatalf("err=%v", err)
			}
			if out.Media != nil {
				t.Fatalf("media=%#v", out.Media)
			}
		})
	}
}

func TestBridgeMediaFieldsMapToAndFromIPC(t *testing.T) {
	consumer := capability.V1MediaSupport()
	mreq := daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}
	in := bridge.Request{ProtocolVersion: 2, Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: "fp", Media: &mreq}
	got := requestV2FromBridge(in)
	if got.ConsumerMedia == nil || got.Media == nil || got.MediaContractFingerprint != "fp" {
		t.Fatalf("request=%#v", got)
	}
}

func TestMediaV2UnixServerClientNegotiatesThenReads(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-ipc-media-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	actions := &mediaActionsFake{result: media.Result{SchemaVersion: 1, Kind: "media", DisplayAddress: media.DisplayAddress{AddressKind: media.AddressCWD, CWD: "/tmp", Path: "a.png"}, MIMEType: "image/png", Format: "png", ByteSize: 3, Width: 1, Height: 1, Data: []byte{1, 2, 3}}}
	srv, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()
	client := NewClient(srv.SocketPath())
	consumer := capability.V1MediaSupport()
	neg, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "neg", Action: "capabilities.negotiate", ConsumerMedia: &consumer})
	if err != nil {
		t.Fatal(err)
	}
	if !neg.OK || neg.NegotiatedMedia == nil {
		t.Fatalf("neg=%#v", neg)
	}
	read, err := client.CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "read", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: neg.NegotiatedMedia.Fingerprint, Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if !read.OK || read.Media == nil || !bytes.Equal(read.Media.Data, []byte{1, 2, 3}) || actions.reads.Load() != 1 {
		t.Fatalf("read=%#v calls=%d", read, actions.reads.Load())
	}
}

func TestMediaV2ClientOuterResponseBoundary(t *testing.T) {
	consumer := capability.V1MediaSupport()
	req := RequestV2{IPVersion: 2, Kind: "request", RequestID: "r", Action: "read_media", ConsumerMedia: &consumer, MediaContractFingerprint: strings.Repeat("a", 64), Media: &daemonapp.MediaRequest{CWD: "/tmp", Path: "a.png"}}
	base := validMediaResponseJSON(req)
	if len(base) >= media.MaxOuterResponseBytes {
		t.Fatal("fixture unexpectedly exceeds outer ceiling")
	}
	exact := append(append([]byte(nil), base...), bytes.Repeat([]byte{' '}, media.MaxOuterResponseBytes-len(base))...)
	out, err := mediaHTTPClient(io.NopCloser(bytes.NewReader(exact))).CallV2(context.Background(), req)
	if err != nil {
		t.Fatalf("exact ceiling: %v", err)
	}
	if out.Media == nil {
		t.Fatal("exact ceiling lost media")
	}
	tooBig := append(exact, ' ')
	out, err = mediaHTTPClient(io.NopCloser(bytes.NewReader(tooBig))).CallV2(context.Background(), req)
	if !errors.Is(err, failure.InvalidDaemonResponse) {
		t.Fatalf("+1 err=%v", err)
	}
	if out.Media != nil {
		t.Fatal("+1 emitted media")
	}
}
