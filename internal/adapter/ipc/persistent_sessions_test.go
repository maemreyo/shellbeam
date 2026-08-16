package ipc

import (
	"bytes"
	"context"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

type persistentSessionIPCActions struct {
	fakeActions
	last persistent.InspectRequest
}

func (a *persistentSessionIPCActions) InspectSessions(_ context.Context, request persistent.InspectRequest) (persistent.InspectPage, error) {
	a.last = request
	return persistent.InspectPage{Sessions: []persistent.Summary{{SessionID: "persistent-ipc-session", OperationID: "persistent-ipc-op", SessionName: "dev-server", Persistent: true, OwnershipStatus: persistent.OwnershipCurrent}}}, nil
}

func TestPersistentSessionIPCV2ClosedSchemaAndInspectDispatch(t *testing.T) {
	start, err := decodeRequestV2(bytes.NewBufferString(`{"ipc_version":2,"kind":"request","request_id":"start","action":"start","operation_id":"persistent-ipc-op","command":"sleep 10","cwd":"/tmp","persistent":true,"session_name":"dev-server"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !start.Persistent || start.SessionName != "dev-server" {
		t.Fatalf("start=%#v", start)
	}

	inspect, err := decodeRequestV2(bytes.NewBufferString(`{"ipc_version":2,"kind":"request","request_id":"inspect","action":"inspect.sessions","session_name":"dev-server","state":"running","persistent_only":false,"max_records":10}`))
	if err != nil {
		t.Fatal(err)
	}
	if inspect.PersistentOnly == nil || *inspect.PersistentOnly || inspect.State != "running" || inspect.MaxRecords != 10 {
		t.Fatalf("inspect=%#v", inspect)
	}
	actions := &persistentSessionIPCActions{}
	server := &Server{actions: actions}
	response := ResponseV2{}
	if err := server.inspectV2(context.Background(), inspect, &response); err != nil {
		t.Fatal(err)
	}
	if actions.last.PersistentOnly == nil || *actions.last.PersistentOnly || actions.last.SessionName != "dev-server" || actions.last.State != "running" {
		t.Fatalf("forwarded=%#v", actions.last)
	}
	if response.Sessions == nil || len(response.Sessions.Sessions) != 1 {
		t.Fatalf("response=%#v", response)
	}
}

func TestPersistentSessionLegacyIPCRejectsB1Fields(t *testing.T) {
	raw := bytes.NewBufferString(`{"ipc_version":1,"request_id":"legacy","payload":{"action":"start","operation_id":"op","command":"true","cwd":"/tmp","persistent":true}}`)
	if _, err := decodeRequest(raw); err == nil {
		t.Fatal("legacy IPC accepted persistent field")
	}
}

var _ interface {
	Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error)
	InspectSessions(context.Context, persistent.InspectRequest) (persistent.InspectPage, error)
} = (*persistentSessionIPCActions)(nil)
