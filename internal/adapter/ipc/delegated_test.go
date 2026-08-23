//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestDelegatedIPCV2DecodeBindsSessionModeAndPositiveAuthorityEpoch(t *testing.T) {
	start, err := decodeRequestV2(strings.NewReader(`{"ipc_version":2,"kind":"request","request_id":"s","action":"start","operation_id":"op","command":"cat","cwd":"/tmp","session_mode":"delegated_interactive","stdin_mode":"stream","timeout_mode":"unlimited"}`))
	if err != nil {
		t.Fatal(err)
	}
	if start.SessionMode != delegated.ModeDelegatedInteractive || start.StdinMode != operation.StdinModeStream || start.TimeoutMode != operation.TimeoutModeUnlimited {
		t.Fatalf("start=%#v", start)
	}
	write, err := decodeRequestV2(strings.NewReader(`{"ipc_version":2,"kind":"request","request_id":"w","action":"write","session_id":"session","authority_epoch":3,"input_offset":0,"chars":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if write.AuthorityEpoch != 3 {
		t.Fatalf("write=%#v", write)
	}
	kill, err := decodeRequestV2(strings.NewReader(`{"ipc_version":2,"kind":"request","request_id":"k","action":"kill","session_id":"session","authority_epoch":4,"kill_id":"kill","signal":"TERM"}`))
	if err != nil {
		t.Fatal(err)
	}
	if kill.AuthorityEpoch != 4 {
		t.Fatalf("kill=%#v", kill)
	}
	for _, raw := range []string{
		`{"ipc_version":2,"kind":"request","request_id":"w0","action":"write","session_id":"session","authority_epoch":0,"input_offset":0,"chars":"x"}`,
		`{"ipc_version":2,"kind":"request","request_id":"k0","action":"kill","session_id":"session","authority_epoch":0,"kill_id":"kill"}`,
	} {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err == nil {
			t.Fatalf("explicit zero epoch accepted: %s", raw)
		}
	}
}

func TestDelegatedBridgeToIPCV2PreservesModeAndEpoch(t *testing.T) {
	start := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: app.StartRequest{ProtocolVersion: 2, OperationID: "op", Command: "cat", CWD: "/tmp", SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited}})
	if start.SessionMode != delegated.ModeDelegatedInteractive || start.StdinMode != operation.StdinModeStream || start.TimeoutMode != operation.TimeoutModeUnlimited {
		t.Fatalf("start=%#v", start)
	}
	write := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "write", Write: app.WriteRequest{SessionID: "s", AuthorityEpoch: 7, InputOffset: 2, Chars: "x"}})
	if write.AuthorityEpoch != 7 {
		t.Fatalf("write=%#v", write)
	}
	kill := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "kill", Kill: app.KillRequest{SessionID: "s", AuthorityEpoch: 8, KillID: "kill", Signal: "TERM"}})
	if kill.AuthorityEpoch != 8 {
		t.Fatalf("kill=%#v", kill)
	}
}

type delegatedIPCActions struct {
	fakeActions
	start app.StartRequest
	write app.WriteRequest
	kill  app.KillRequest
}

func (a *delegatedIPCActions) Start(_ context.Context, req app.StartRequest) (app.View, error) {
	a.start = req
	return app.View{OperationID: req.OperationID, SessionID: "s", State: "running", AuthorityEpoch: 1}, nil
}
func (a *delegatedIPCActions) Write(_ context.Context, req app.WriteRequest) (app.View, error) {
	a.write = req
	return app.View{SessionID: req.SessionID, State: "running", AuthorityEpoch: req.AuthorityEpoch}, nil
}
func (a *delegatedIPCActions) Kill(_ context.Context, req app.KillRequest) (app.View, error) {
	a.kill = req
	return app.View{SessionID: req.SessionID, State: "running", AuthorityEpoch: req.AuthorityEpoch}, nil
}

func TestDelegatedIPCV2DispatchPreservesModeAndEpochIntoDaemon(t *testing.T) {
	a := &delegatedIPCActions{}
	s := &Server{actions: a}
	resp := &ResponseV2{}
	if err := s.dispatchV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "s", Action: "start", OperationID: "op", Command: "cat", CWD: "/tmp", SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited}, resp); err != nil {
		t.Fatal(err)
	}
	if a.start.SessionMode != delegated.ModeDelegatedInteractive || a.start.StdinMode != operation.StdinModeStream || a.start.TimeoutMode != operation.TimeoutModeUnlimited {
		t.Fatalf("start=%#v", a.start)
	}
	if resp.Result == nil || resp.Result.SessionMode != delegated.ModeDelegatedInteractive || resp.Result.AuthorityEpoch != 1 || resp.Result.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly || resp.Result.InputAuthorityProvenance != receipt.InputAuthorityAgentOnly {
		t.Fatalf("live delegated result=%#v", resp.Result)
	}
	if err := s.dispatchV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "w", Action: "write", SessionID: "s", AuthorityEpoch: 9, InputOffset: 0, Chars: "x"}, &ResponseV2{}); err != nil {
		t.Fatal(err)
	}
	if a.write.AuthorityEpoch != 9 {
		t.Fatalf("write=%#v", a.write)
	}
	if err := s.dispatchV2(context.Background(), RequestV2{IPVersion: 2, Kind: "request", RequestID: "k", Action: "kill", SessionID: "s", AuthorityEpoch: 10, KillID: "kill", Signal: "TERM"}, &ResponseV2{}); err != nil {
		t.Fatal(err)
	}
	if a.kill.AuthorityEpoch != 10 {
		t.Fatalf("kill=%#v", a.kill)
	}
}

func TestDelegatedIPCV2RejectsUnknownOrIllegalStartModeBeforeDispatch(t *testing.T) {
	for _, req := range []RequestV2{
		{IPVersion: 2, Kind: "request", RequestID: "unknown", Action: "start", OperationID: "op", Command: "cat", CWD: "/tmp", SessionMode: "future_mode"},
		{IPVersion: 2, Kind: "request", RequestID: "tty", Action: "start", OperationID: "op", Command: "cat", CWD: "/tmp", SessionMode: delegated.ModeDelegatedInteractive, TTY: true},
		{IPVersion: 2, Kind: "request", RequestID: "persistent", Action: "start", OperationID: "op", Command: "cat", CWD: "/tmp", SessionMode: delegated.ModeDelegatedInteractive, Persistent: true},
	} {
		if err := validateRequestV2(req); err == nil {
			t.Fatalf("invalid request accepted: %#v", req)
		}
	}
	_ = errors.Is
}

type delegatedLegacyIPCActions struct{ fakeActions }

func (delegatedLegacyIPCActions) Poll(context.Context, app.PollRequest) (app.View, error) {
	return app.View{
		SessionID:      "delegated-v1",
		State:          "completed",
		AuthorityEpoch: 7,
		Receipt: &receipt.Receipt{
			SchemaVersion: 5, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 7,
		},
	}, nil
}

func TestDelegatedIPCV1ResponseOmitsModernAuthorityAndReceipt(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("/tmp", "shellbeam-ipc-v1-delegated-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	srv, err := Listen(runtimeDir, delegatedLegacyIPCActions{})
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()
	client := NewClient(srv.SocketPath())
	got, err := client.Call(context.Background(), Request{IPVersion: 1, RequestID: "legacy-delegated", Payload: Action{Action: "poll", SessionID: "delegated-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.View.SessionID != "delegated-v1" {
		t.Fatalf("legacy response=%#v", got)
	}
	if got.View.AuthorityEpoch != 0 || got.View.Receipt != nil {
		t.Fatalf("legacy IPC leaked H1 metadata: %#v", got.View)
	}
}
