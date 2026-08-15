package ipc

import (
	"context"
	"errors"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	"github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

func TestA25RequestV2DecodesDistinctEnvironmentAndProcessShapes(t *testing.T) {
	envRaw := `{"ipc_version":2,"kind":"request","request_id":"env","action":"inspect.environment","freshness":"refresh","execution":{"mode":"argv","identity":"go"}}`
	envReq, err := decodeRequestV2(strings.NewReader(envRaw))
	if err != nil {
		t.Fatal(err)
	}
	if envReq.Freshness != environment.FreshnessRefresh || envReq.Execution == nil || envReq.Execution.Mode != "argv" || envReq.Execution.Identity != "go" || envReq.ProcessTarget != nil {
		t.Fatalf("environment request=%#v", envReq)
	}

	procRaw := `{"ipc_version":2,"kind":"request","request_id":"proc","action":"inspect.process","process_target":{"kind":"pid","pid":42},"include_ports":true}`
	procReq, err := decodeRequestV2(strings.NewReader(procRaw))
	if err != nil {
		t.Fatal(err)
	}
	if procReq.ProcessTarget == nil || procReq.ProcessTarget.Kind != processcore.TargetPID || procReq.ProcessTarget.PID != 42 || !procReq.IncludePorts || procReq.Execution != nil {
		t.Fatalf("process request=%#v", procReq)
	}

	for _, raw := range []string{
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"inspect.environment","process_target":{"kind":"pid","pid":1}}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"inspect.process","target":{"kind":"session","session_id":"s"}}`,
	} {
		if _, err := decodeRequestV2(strings.NewReader(raw)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("cross-action request accepted %s: %v", raw, err)
		}
	}
}

func TestA25BridgeRequestV2MappingIsLossless(t *testing.T) {
	execution := environment.ExecutionContext{Mode: "shell", Identity: "/bin/zsh"}
	envReq := bridge.Request{ProtocolVersion: 2, Action: "inspect.environment", EnvironmentInspect: environmentapp.InspectRequest{WorkspaceID: "ws_01K00000000000000000000000", Freshness: environment.FreshnessRefresh, Execution: &execution}}
	encodedEnv := requestV2FromBridge(envReq)
	if encodedEnv.WorkspaceID != envReq.EnvironmentInspect.WorkspaceID || encodedEnv.Freshness != environment.FreshnessRefresh || encodedEnv.Execution == nil || *encodedEnv.Execution != execution {
		t.Fatalf("encoded environment=%#v", encodedEnv)
	}
	execution.Identity = "mutated"
	if encodedEnv.Execution.Identity != "/bin/zsh" {
		t.Fatal("environment execution pointer aliased bridge request")
	}

	target := processcore.Target{Kind: processcore.TargetSession, SessionID: "session-123"}
	procReq := bridge.Request{ProtocolVersion: 2, Action: "inspect.process", ProcessInspect: processapp.InspectRequest{Target: target, IncludePorts: true}}
	encodedProc := requestV2FromBridge(procReq)
	if encodedProc.ProcessTarget == nil || *encodedProc.ProcessTarget != target || !encodedProc.IncludePorts {
		t.Fatalf("encoded process=%#v", encodedProc)
	}
}

type a25BaseActions struct{}

func (a25BaseActions) Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (a25BaseActions) Poll(context.Context, daemonapp.PollRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (a25BaseActions) Write(context.Context, daemonapp.WriteRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (a25BaseActions) Kill(context.Context, daemonapp.KillRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (a25BaseActions) InspectServer(context.Context) (daemonapp.ServerInfo, error) {
	return daemonapp.ServerInfo{}, nil
}

func TestA25MissingOptionalIPCActionsStayFeatureUnavailable(t *testing.T) {
	server := &Server{actions: a25BaseActions{}}
	for _, req := range []RequestV2{
		{Action: "inspect.environment"},
		{Action: "inspect.process", ProcessTarget: &processcore.Target{Kind: processcore.TargetPID, PID: 1}},
	} {
		var resp ResponseV2
		err := server.inspectV2(context.Background(), req, &resp)
		if !errors.Is(err, failure.FeatureUnavailable) {
			t.Fatalf("action %s err=%v", req.Action, err)
		}
	}
}

type a25ObservationActions struct {
	a25BaseActions
	envReq  environmentapp.InspectRequest
	procReq processapp.InspectRequest
}

func (a *a25ObservationActions) InspectEnvironment(_ context.Context, req EnvironmentRequest) (EnvironmentResponse, error) {
	a.envReq = req
	return environment.Snapshot{SchemaVersion: environment.SnapshotSchemaVersion, Quality: environment.QualityComplete}, nil
}
func (a *a25ObservationActions) InspectProcess(_ context.Context, req ProcessRequest) (ProcessResponse, error) {
	a.procReq = req
	return processcore.Observation{SchemaVersion: processcore.SchemaVersion, Quality: processcore.QualityComplete, Target: req.Target}, nil
}

func TestA25IPCActionsPreserveTypedRequestsAndClearPayloadOnError(t *testing.T) {
	actions := &a25ObservationActions{}
	server := &Server{actions: actions}
	execution := environment.ExecutionContext{Mode: "argv", Identity: "go"}
	envReq := RequestV2{Action: "inspect.environment", Freshness: environment.FreshnessRefresh, Execution: &execution}
	var envResp ResponseV2
	if err := server.inspectV2(context.Background(), envReq, &envResp); err != nil {
		t.Fatal(err)
	}
	if envResp.Environment == nil || actions.envReq.Freshness != environment.FreshnessRefresh || actions.envReq.Execution == nil || *actions.envReq.Execution != execution {
		t.Fatalf("env response=%#v request=%#v", envResp.Environment, actions.envReq)
	}

	target := processcore.Target{Kind: processcore.TargetPID, PID: 123}
	procReq := RequestV2{Action: "inspect.process", ProcessTarget: &target, IncludePorts: true}
	var procResp ResponseV2
	if err := server.inspectV2(context.Background(), procReq, &procResp); err != nil {
		t.Fatal(err)
	}
	if procResp.Process == nil || actions.procReq.Target != target || !actions.procReq.IncludePorts {
		t.Fatalf("process response=%#v request=%#v", procResp.Process, actions.procReq)
	}

	clearResponseV2Payload(&ResponseV2{Environment: envResp.Environment, Process: procResp.Process})
	response := ResponseV2{Environment: envResp.Environment, Process: procResp.Process}
	clearResponseV2Payload(&response)
	if response.Environment != nil || response.Process != nil {
		t.Fatalf("new payload survived error clear: %#v", response)
	}
}
