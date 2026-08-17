package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const ipcCheckpointID = "chk_01K00000000000000000000000"
const ipcCheckpointWorkspace = "ws_01K00000000000000000000000"

func TestE26RequestV2DecodesClosedCheckpointShapes(t *testing.T) {
	valid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"create","action":"checkpoint_create","checkpoint_create_id":"cp-create-1","workspace_id":"ws_01K00000000000000000000000","activity_id":"PI-756","paths":["src/**","README.md"]}`,
		`{"ipc_version":2,"kind":"request","request_id":"restore","action":"checkpoint_restore","restore_id":"restore-1","checkpoint_id":"chk_01K00000000000000000000000","paths":["src/main.go"]}`,
		`{"ipc_version":2,"kind":"request","request_id":"inspect","action":"checkpoint_inspect","checkpoint_id":"chk_01K00000000000000000000000"}`,
	}
	for _, raw := range valid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); err != nil {
			t.Errorf("valid checkpoint request rejected %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"checkpoint_create","checkpoint_create_id":"cp-create-1","workspace_id":"ws_01K00000000000000000000000","paths":["src"],"checkpoint_id":"chk_01K00000000000000000000000"}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"checkpoint_restore","restore_id":"restore-1","checkpoint_id":"chk_01K00000000000000000000000","paths":["src/**"]}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"checkpoint_inspect","checkpoint_id":"chk_01K00000000000000000000000","paths":["src"]}`,
	}
	for _, raw := range invalid {
		if _, err := decodeRequestV2(strings.NewReader(raw)); !errors.Is(err, failure.InvalidInput) {
			t.Errorf("invalid checkpoint request accepted %s: %v", raw, err)
		}
	}
}

func TestE26BridgeRequestV2MappingIsLossless(t *testing.T) {
	create := core.CreateRequest{CreateID: "cp-create-1", WorkspaceID: ipcCheckpointWorkspace, ActivityID: "PI-756", Paths: []string{"src/**", "README.md"}}
	encoded := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "checkpoint_create", CheckpointCreate: create})
	if encoded.CheckpointCreateID != create.CreateID || encoded.WorkspaceID != create.WorkspaceID || encoded.ActivityID != create.ActivityID || !reflect.DeepEqual(encoded.Paths, create.Paths) {
		t.Fatalf("create mapping=%#v", encoded)
	}
	create.Paths[0] = "mutated"
	if encoded.Paths[0] != "src/**" {
		t.Fatal("checkpoint create paths aliased bridge request")
	}

	restore := core.RestoreRequest{RestoreID: "restore-1", CheckpointID: ipcCheckpointID, Paths: []string{"src/main.go"}}
	encoded = requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "checkpoint_restore", CheckpointRestore: restore})
	if encoded.RestoreID != restore.RestoreID || encoded.CheckpointID != restore.CheckpointID || !reflect.DeepEqual(encoded.Paths, restore.Paths) {
		t.Fatalf("restore mapping=%#v", encoded)
	}
	encoded = requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "checkpoint_inspect", CheckpointID: ipcCheckpointID})
	if encoded.CheckpointID != ipcCheckpointID {
		t.Fatalf("inspect mapping=%#v", encoded)
	}
}

type e26CheckpointActions struct {
	a25BaseActions
	starts  int
	create  core.CreateRequest
	restore core.RestoreRequest
	inspect string
}

func (a *e26CheckpointActions) CreateCheckpoint(_ context.Context, req core.CreateRequest) (core.Checkpoint, error) {
	a.create = req
	return checkpointFixture(req), nil
}
func (a *e26CheckpointActions) RestoreCheckpoint(_ context.Context, req core.RestoreRequest) (core.RestoreResult, error) {
	a.restore = req
	return core.RestoreResult{SchemaVersion: core.SchemaVersion, RestoreID: req.RestoreID, CheckpointID: req.CheckpointID, Paths: []core.RestorePathResult{{Path: req.Paths[0], Outcome: core.RestoreRestored}}, Complete: true}, nil
}
func (a *e26CheckpointActions) InspectCheckpoint(_ context.Context, checkpointID string) (checkpointapp.CheckpointInspection, error) {
	a.inspect = checkpointID
	cp := checkpointFixture(core.CreateRequest{CreateID: "cp-create-1", WorkspaceID: ipcCheckpointWorkspace, ActivityID: "PI-756"})
	return checkpointapp.CheckpointInspection{Checkpoint: cp, Provider: checkpointapp.ProviderCheckpointStatus{CheckpointID: checkpointID, RetentionState: core.RetentionAvailable, Available: true}}, nil
}

func TestE26IPCRoutesTypedCheckpointActionsAndMissingCompositionIsUnavailable(t *testing.T) {
	actions := &e26CheckpointActions{}
	server := &Server{actions: actions}
	createReq := RequestV2{Action: "checkpoint_create", CheckpointCreateID: "cp-create-1", WorkspaceID: ipcCheckpointWorkspace, ActivityID: "PI-756", Paths: []string{"src/**"}}
	var createResp ResponseV2
	if err := server.checkpointV2(context.Background(), createReq, &createResp); err != nil {
		t.Fatal(err)
	}
	if createResp.Checkpoint == nil || actions.create.CreateID != "cp-create-1" || actions.starts != 0 {
		t.Fatalf("create response=%#v request=%#v starts=%d", createResp.Checkpoint, actions.create, actions.starts)
	}

	restoreReq := RequestV2{Action: "checkpoint_restore", RestoreID: "restore-1", CheckpointID: ipcCheckpointID, Paths: []string{"src/main.go"}}
	var restoreResp ResponseV2
	if err := server.checkpointV2(context.Background(), restoreReq, &restoreResp); err != nil {
		t.Fatal(err)
	}
	if restoreResp.Restore == nil || actions.restore.RestoreID != "restore-1" || actions.starts != 0 {
		t.Fatalf("restore response=%#v request=%#v starts=%d", restoreResp.Restore, actions.restore, actions.starts)
	}

	var inspectResp ResponseV2
	if err := server.checkpointV2(context.Background(), RequestV2{Action: "checkpoint_inspect", CheckpointID: ipcCheckpointID}, &inspectResp); err != nil {
		t.Fatal(err)
	}
	if inspectResp.CheckpointInspection == nil || actions.inspect != ipcCheckpointID || actions.starts != 0 {
		t.Fatalf("inspect response=%#v inspect=%q starts=%d", inspectResp.CheckpointInspection, actions.inspect, actions.starts)
	}

	missing := &Server{actions: a25BaseActions{}}
	for _, req := range []RequestV2{{Action: "checkpoint_create"}, {Action: "checkpoint_restore"}, {Action: "checkpoint_inspect"}} {
		var resp ResponseV2
		if err := missing.checkpointV2(context.Background(), req, &resp); !errors.Is(err, failure.FeatureUnavailable) {
			t.Fatalf("%s err=%v", req.Action, err)
		}
	}
}

func TestE26ClearResponseDropsCheckpointPayloads(t *testing.T) {
	checkpoint := core.Checkpoint{CheckpointID: ipcCheckpointID}
	restore := core.RestoreResult{CheckpointID: ipcCheckpointID}
	inspection := checkpointapp.CheckpointInspection{}
	resp := ResponseV2{Checkpoint: &checkpoint, Restore: &restore, CheckpointInspection: &inspection}
	clearResponseV2Payload(&resp)
	if resp.Checkpoint != nil || resp.Restore != nil || resp.CheckpointInspection != nil {
		t.Fatalf("checkpoint payload survived clear: %#v", resp)
	}
}

func checkpointFixture(req core.CreateRequest) core.Checkpoint {
	return core.Checkpoint{
		SchemaVersion: core.SchemaVersion, CheckpointID: ipcCheckpointID, CreateID: req.CreateID,
		Provider: core.ProviderIdentity{ID: "localfs", Version: 1}, WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID,
		SourceGeneration: "gen_" + strings.Repeat("a", 64), CreatedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		CapturedPathCount: 1, TotalBytes: 7, CaptureQuality: core.CaptureComplete, RetentionState: core.RetentionAvailable,
		OpaqueEntryRefs: []string{"entry_01K00000000000000000000000"},
	}
}

func TestE26LegacyIPCV1RejectsCheckpointActions(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	server := &Server{actions: a25BaseActions{}, ready: ready, closing: make(chan struct{})}
	for _, action := range []string{"checkpoint_create", "checkpoint_restore", "checkpoint_inspect"} {
		raw := `{"ipc_version":1,"request_id":"legacy","payload":{"action":"` + action + `"}}`
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/v1/local-shell", strings.NewReader(raw))
		server.handle(recorder, request)
		var response Response
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.OK || response.Error == nil || response.Error.Code != string(failure.InvalidInput) {
			t.Fatalf("legacy IPC action %s response=%#v", action, response)
		}
	}
}
