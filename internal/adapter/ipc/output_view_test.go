//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
)

type outputViewActions struct {
	fakeActions
	startCalls int
	last       outputview.Request
	result     outputview.Result
}

func (a *outputViewActions) Start(ctx context.Context, req daemonapp.StartRequest) (daemonapp.View, error) {
	a.startCalls++
	return a.fakeActions.Start(ctx, req)
}
func (a *outputViewActions) ReadOutputView(_ context.Context, req outputview.Request) (outputview.Result, error) {
	a.last = req
	return a.result, nil
}

func TestOutputViewIPCV2DecodeAndBridgeRoundTrip(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"out","action":"read_output","session_id":"s","selector":{"kind":"search","mode":"literal","pattern":"boom","case_sensitive":true,"max_matches":2},"continuation":"outcur_v1_abc.def"}`)
	got, err := decodeRequestV2(bytesReaderV2(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.Selector == nil || got.Selector.Kind != outputview.SelectorSearch || got.Selector.Pattern != "boom" || got.Continuation != "outcur_v1_abc.def" {
		t.Fatalf("decoded=%#v", got)
	}

	bridgeReq := bridge.Request{ProtocolVersion: 2, Action: "read_output", OutputRead: outputview.Request{SessionID: "s", Selector: *got.Selector, Continuation: got.Continuation}}
	forwarded := requestV2FromBridge(bridgeReq)
	if err := validateRequestV2(forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.Selector == nil || forwarded.Selector.Pattern != "boom" || forwarded.SessionID != "s" || forwarded.Continuation != "outcur_v1_abc.def" {
		t.Fatalf("forwarded=%#v", forwarded)
	}
}

func TestOutputViewIPCV2RejectsCrossActionAndInvalidSelector(t *testing.T) {
	invalid := [][]byte{
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"out","action":"read_output","session_id":"s"}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"out","action":"read_output","session_id":"s","selector":{"kind":"raw_range","max_bytes":0}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"out","action":"read_output","session_id":"s","cursor":1,"selector":{"kind":"raw_range","max_bytes":1}}`),
		[]byte(`{"ipc_version":2,"kind":"request","request_id":"out","action":"poll","session_id":"s","selector":{"kind":"raw_range","max_bytes":1}}`),
	}
	for _, raw := range invalid {
		if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
			t.Fatalf("invalid request accepted: %s", raw)
		}
	}
}

func TestOutputViewIPCV2UsesOutputActionsWithoutSpawn(t *testing.T) {
	actions := &outputViewActions{result: outputview.Result{SchemaVersion: 1, SessionID: "s", SelectorKind: outputview.SelectorRawRange, RetentionState: outputview.RetentionRetained, FrozenCutBytes: 4, Text: "boom"}}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-output-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	server, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()
	sel := outputview.Selector{Kind: outputview.SelectorRawRange, MaxBytes: 4}
	got, err := NewClient(server.SocketPath()).CallV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "out", Action: "read_output", SessionID: "s", Selector: &sel})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.OutputView == nil || got.OutputView.Text != "boom" || actions.startCalls != 0 {
		t.Fatalf("response=%#v starts=%d", got, actions.startCalls)
	}
	if actions.last.SessionID != "s" || actions.last.Selector.Kind != outputview.SelectorRawRange {
		t.Fatalf("request=%#v", actions.last)
	}
}
