package mcp

import (
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func requestFromInput(version int, in input, raw []byte) bridge.Request {
	request := bridge.Request{Action: in.Action}
	if version == 2 || in.Action == "inspect.server" {
		request.ProtocolVersion = 2
	}
	if !hasField(raw, "yield_time_ms") && in.Action == "start" {
		in.YieldMS = 10000
	}
	if !hasField(raw, "max_output_bytes") {
		in.MaxOutputBytes = 20000
	}
	populateRequestFromInput(&request, in)
	return request
}

func populateRequestFromInput(request *bridge.Request, in input) {
	if isDecisionProtocolMCPAction(in.Action) {
		request.WorkspaceID = in.WorkspaceID
		request.Decision = cloneDecisionMCPRequest(in.Decision)
		return
	}
	switch in.Action {
	case "read_media":
		request.Media = mediaRequestFromInput(in)
	case "context.exec":
		request.ContextExec = contextcore.Request{ContextExecID: in.ContextExecID, SessionID: in.SessionID, AuthorityEpoch: in.AuthorityEpoch, Argv: append([]string(nil), in.Argv...), TimeoutMS: in.TimeoutMS, MaxOutputBytes: int64(in.MaxOutputBytes)}
	case "start":
		request.Start = app.StartRequest{OperationID: in.OperationID, ActivityID: in.ActivityID, ExperimentID: in.ExperimentID, WorkspaceID: in.WorkspaceID, WorkspaceHint: in.WorkspaceHint, StructuredAdapter: in.StructuredAdapter, ProjectCommandID: in.ProjectCommandID, Params: cloneMCPStringMap(in.Params), Command: in.Command, Argv: append([]string(nil), in.Argv...), Intent: in.Intent, Evidence: in.Evidence, VerificationAttempt: cloneMCPVerificationAttempt(in.VerificationAttempt), CWD: in.CWD, TTY: in.TTY, Persistent: in.Persistent, SessionMode: in.SessionMode, SessionName: in.SessionName, YieldMS: in.YieldMS, TimeoutMS: in.TimeoutMS, StdinMode: in.StdinMode, TimeoutMode: in.TimeoutMode, MaxOutputBytes: in.MaxOutputBytes, TraceMode: in.TraceMode, ResourceLimits: in.ResourceLimits.Clone(), Hermetic: in.Hermetic.Clone()}
	case "handoff.request":
		request.HandoffRequest = handoff.Request{HandoffID: in.HandoffID, SessionID: in.SessionID, Reason: handoff.Reason(in.Reason), Privacy: in.HandoffPrivacy, Completion: *in.HandoffCompletion}
	case "handoff.wait":
		request.HandoffWait = handoffapp.WaitRequest{HandoffID: in.HandoffID, Yield: time.Duration(in.YieldMS) * time.Millisecond}
	case "handoff.abort", "inspect.handoff":
		request.HandoffID = in.HandoffID
	case "poll":
		request.Poll = app.PollRequest{SessionID: in.SessionID, Cursor: in.Cursor, YieldMS: in.YieldMS, MaxOutputBytes: in.MaxOutputBytes}
	case "read_output":
		request.OutputRead.SessionID = in.SessionID
		request.OutputRead.Selector = *in.Selector
		request.OutputRead.Continuation = in.Continuation
	case "write":
		request.Write = app.WriteRequest{SessionID: in.SessionID, AuthorityEpoch: in.AuthorityEpoch, InputOffset: in.InputOffset, Chars: in.Chars, EOF: in.EOF}
	case "inspect.verification", "verification.policy.preview", "verification.policy.activate", "verification.waiver.set", "verification.waiver.revoke":
		applyVerificationInput(request, in)
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		request.WorkspaceID = in.WorkspaceID
	case "inspect.activity":
		request.ActivityID = in.ActivityID
	case "inspect.sessions":
		request.SessionInspect = persistent.InspectRequest{SessionName: in.SessionName, ActivityID: in.ActivityID, WorkspaceID: in.WorkspaceID, State: in.State, Limit: in.MaxRecords, Cursor: in.Continuation}
		if in.PersistentOnly != nil {
			value := *in.PersistentOnly
			request.SessionInspect.PersistentOnly = &value
		}
	case "mutation_scope.set":
		request.MutationScopeSet = mutationapp.SetRequest{MutationID: in.MutationID, ScopeID: in.ScopeID, ActivityID: in.ActivityID, WorkspaceID: workspacecore.WorkspaceID(in.WorkspaceID), Mode: in.Mode, Paths: append([]string(nil), in.Paths...), TTLMS: in.TTLMS}
	case "mutation_scope.release":
		request.MutationScopeRelease = mutationapp.ReleaseRequest{MutationID: in.MutationID, ScopeID: in.ScopeID}
	case "inspect.mutation_scopes":
		request.MutationScopeInspect = mutationapp.InspectRequest{WorkspaceID: workspacecore.WorkspaceID(in.WorkspaceID), ActivityID: in.ActivityID}
	case "inspect.events":
		request.EventInspect = observationapp.InspectRequest{Target: *in.Target, AfterEventCursor: in.AfterEventCursor, MaxEvents: in.MaxEvents}
	case "inspect.code":
		request.WorkspaceID = in.WorkspaceID
		request.ActivityID = in.ActivityID
		if in.CodeQuery != nil {
			query := *in.CodeQuery
			request.CodeQuery = &query
		}
	case "inspect.structured", "inspect.evidence", "inspect.environment", "inspect.process":
		populateDataInspectionRequest(request, in)
	case "checkpoint_create", "checkpoint_restore", "checkpoint_inspect":
		applyCheckpointInput(request, in)
	case "inspect.trace":
		request.InputTraceInspect.OperationID = in.OperationID
		request.InputTraceInspect.MaxResources = in.MaxResources
	case "inspect.telemetry":
		request.TelemetryInspect = telemetryapp.InspectRequest{OperationID: in.OperationID, MaxSamples: in.MaxSamples}
	case "repro.create":
		policy := reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}
		if in.CapturePolicy != nil {
			policy = *in.CapturePolicy
		}
		request.ReproCreate = reprocore.CreateRequest{CreateID: in.ReproCreateID, OperationID: in.OperationID, Policy: policy}
	case "inspect.repro":
		request.ReproID = in.ReproID
	case "kill":
		applyKillInput(request, in)
	}
}

func populateDataInspectionRequest(request *bridge.Request, in input) {
	switch in.Action {
	case "inspect.structured":
		request.StructuredInspect = structuredapp.InspectRequest{OperationID: in.OperationID, Filter: structuredapp.RecordFilter{RecordKind: in.RecordKind, Severity: in.Severity, Path: in.Path, TestStatus: in.TestStatus}, Continuation: in.Continuation, MaxRecords: in.MaxRecords}
	case "inspect.evidence":
		request.EvidenceInspect = evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{EvidenceID: in.EvidenceID, OperationID: in.OperationID, WorkspaceID: in.WorkspaceID, ProjectCommandID: in.ProjectCommandID, ActivityID: in.ActivityID, VerificationKind: in.VerificationKind, Result: in.EvidenceResult, RevalidateArtifacts: in.RevalidateArtifacts}, Continuation: in.Continuation, MaxRecords: in.MaxRecords}
	case "inspect.environment":
		request.EnvironmentInspect = environmentapp.InspectRequest{WorkspaceID: in.WorkspaceID, Freshness: in.Freshness}
		if in.Execution != nil {
			execution := *in.Execution
			request.EnvironmentInspect.Execution = &execution
		}
	case "inspect.process":
		request.ProcessInspect = processapp.InspectRequest{Target: *in.ProcessTarget, IncludePorts: in.IncludePorts}
	}
}

func applyKillInput(request *bridge.Request, in input) {
	signal := in.Signal
	if signal == "" {
		signal = "TERM"
	}
	request.Kill = app.KillRequest{SessionID: in.SessionID, AuthorityEpoch: in.AuthorityEpoch, KillID: in.KillID, Signal: signal}
}
func cloneMCPVerificationAttempt(value *coreevidence.VerificationAttemptIntent) *coreevidence.VerificationAttemptIntent {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
