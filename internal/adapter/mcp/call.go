package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func call(ctx context.Context, h *bridge.Handler, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	in, err := decodeInput(req.Params.Arguments)
	version := protocolGeneration(req.ProtocolVersion())
	if err != nil {
		return versionedToolError(version, "", "invalid_request", "invalid request", false), nil
	}
	if version == 2 && isDeferredAction(in.Action) {
		return toolErrorV2(in.Action, "feature_unavailable", "feature unavailable", false), nil
	}
	if err := validateForVersion(version, in, req.Params.Arguments); err != nil {
		return versionedToolError(version, in.Action, "invalid_request", err.Error(), false), nil
	}
	request := requestFromInput(version, in, req.Params.Arguments)
	out, err := h.Handle(ctx, request)
	if err != nil {
		return versionedToolError(version, in.Action, "daemon_unavailable", "daemon request failed", true), nil
	}
	if out.Code != "" {
		return versionedToolError(version, in.Action, out.Code, out.Message, out.Retryable), nil
	}
	if version == 2 {
		return successV2(in.Action, out), nil
	}
	return successV1(in.Action, out), nil
}

func decodeInput(raw []byte) (input, error) {
	var in input
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return input{}, err
	}
	return in, nil
}

func requestFromInput(version int, in input, raw []byte) bridge.Request {
	request := bridge.Request{Action: in.Action}
	if version == 2 || in.Action == "inspect.server" {
		request.ProtocolVersion = 2
	}
	yieldMS, maxOutput := in.YieldMS, in.MaxOutputBytes
	if !hasField(raw, "yield_time_ms") && in.Action == "start" {
		yieldMS = 10000
	}
	if !hasField(raw, "max_output_bytes") {
		maxOutput = 20000
	}
	switch in.Action {
	case "start":
		request.Start = app.StartRequest{OperationID: in.OperationID, ActivityID: in.ActivityID, WorkspaceID: in.WorkspaceID, WorkspaceHint: in.WorkspaceHint, StructuredAdapter: in.StructuredAdapter, ProjectCommandID: in.ProjectCommandID, Params: cloneMCPStringMap(in.Params), Command: in.Command, Argv: append([]string(nil), in.Argv...), Intent: in.Intent, Evidence: in.Evidence, CWD: in.CWD, TTY: in.TTY, Persistent: in.Persistent, SessionName: in.SessionName, YieldMS: yieldMS, TimeoutMS: in.TimeoutMS, StdinMode: in.StdinMode, TimeoutMode: in.TimeoutMode, MaxOutputBytes: maxOutput}
	case "poll":
		request.Poll = app.PollRequest{SessionID: in.SessionID, Cursor: in.Cursor, YieldMS: yieldMS, MaxOutputBytes: maxOutput}
	case "read_output":
		request.OutputRead.SessionID = in.SessionID
		request.OutputRead.Selector = *in.Selector
		request.OutputRead.Continuation = in.Continuation
	case "write":
		request.Write = app.WriteRequest{SessionID: in.SessionID, InputOffset: in.InputOffset, Chars: in.Chars, EOF: in.EOF}
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
		signal := in.Signal
		if signal == "" {
			signal = "TERM"
		}
		request.Kill = app.KillRequest{SessionID: in.SessionID, KillID: in.KillID, Signal: signal}
	}
	return request
}

func successV1(action string, out bridge.Response) *mcpgo.CallToolResult {
	if action == "inspect.server" {
		if out.Server == nil {
			return toolError("invalid_daemon_response", "server catalog missing", false)
		}
		body := map[string]any{"schema_version": 1, "ok": true, "action": action, "server": legacyCatalogView(*out.Server)}
		return toolSuccess("inspect.server: capabilities", body)
	}
	body := map[string]any{
		"schema_version": 1, "ok": true, "action": action,
		"session_id": out.View.SessionID, "state": out.View.State, "outcome": out.View.Outcome,
		"output": out.View.Output, "cursor": out.View.Cursor, "next_cursor": out.View.NextCursor,
		"truncated": out.View.Truncated, "accepted_input_bytes": out.View.AcceptedInputBytes,
		"next_input_offset": out.View.NextInputOffset, "eof_queued": out.View.EOFQueued,
		"kill_id": out.View.KillID, "signal": out.View.Signal, "receipt": out.View.Receipt,
	}
	if out.View.OperationID != "" {
		body["operation_id"] = out.View.OperationID
	}
	return toolSuccess(fmt.Sprintf("%s session %s: %s", action, out.View.SessionID, out.View.State), body)
}

func legacyCatalogView(c capability.Catalog) capability.Catalog {
	out := c.Clone()
	out.EventCursorSchemaVersions = nil
	out.ResultCursorSchemaVersions = nil
	out.StructuredAdapterIDs = nil
	out.StructuredResultKinds = nil
	out.StructuredLifecycle = false
	out.TelemetrySchemaVersions = nil
	out.ReproSchemaVersions = nil
	out.ReadinessSchemaVersions = nil
	out.OutputViewSchemaVersions = nil
	out.ReadinessRequirementKinds = nil
	out.TypedCommandVersions = nil
	out.TypedCommandManifestVersion = 0
	out.TypedCommandParameterKinds = nil
	out.TypedCommandPackageProviders = nil
	filteredReceipts := out.ReceiptSchemaVersions[:0]
	for _, version := range out.ReceiptSchemaVersions {
		if version <= 2 {
			filteredReceipts = append(filteredReceipts, version)
		}
	}
	out.ReceiptSchemaVersions = filteredReceipts
	out.ResourceObservation = nil
	out.Limits.TelemetryMaxSamples = 0
	out.Limits.TelemetryMetadataBytes = 0
	out.Limits.TelemetryMaxKeys = 0
	out.Limits.TelemetryMaxKeysPerRepository = 0
	out.Limits.TelemetryMaxSamplesPerKey = 0
	out.Limits.TelemetryRetentionAgeMS = 0
	out.Limits.TelemetryInspectSamples = 0
	out.Limits.ReproMaxCapsules = 0
	out.Limits.ReproMaxReferences = 0
	out.Limits.ReproMetadataBytes = 0
	out.Limits.ReadinessCacheTTLMS = 0
	out.Limits.ReadinessCacheEntries = 0
	out.Limits.OutputViewMaxReturnBytes = 0
	out.Limits.OutputViewMaxWorkBytes = 0
	out.Limits.OutputViewMaxLines = 0
	out.Limits.OutputViewMaxMatches = 0
	out.Limits.OutputViewMaxPatternBytes = 0
	out.Limits.OutputViewMaxContinuationBytes = 0
	out.Limits.EventJournalMaxEvents = 0
	out.Limits.EventCursorBytes = 0
	out.Limits.EventSnapshotFacts = 0
	out.Limits.StructuredInspectRecords = 0
	delete(out.Features, capability.FeatureEventJournal)
	delete(out.Features, capability.FeatureEventSnapshotRecovery)
	delete(out.Features, capability.FeatureStructuredResults)
	delete(out.Features, capability.FeatureStructuredLifecycle)
	delete(out.Features, capability.FeatureCodeIntelligence)
	delete(out.Features, capability.FeatureExecutionTelemetry)
	delete(out.Features, capability.FeatureReproductionCapsules)
	delete(out.Features, capability.FeatureProjectReadiness)
	delete(out.Features, capability.FeatureTypedProjectCommands)
	delete(out.Features, capability.FeatureOutputViews)
	delete(out.Features, capability.FeatureMutationScopes)
	out.MutationScopeSchemaVersions = nil
	stripLegacyPersistentCapabilities(&out)
	out.Limits.MutationScopeActivePerActivity = 0
	out.Limits.MutationScopeActivePerWorkspace = 0
	out.Limits.MutationScopePathsPerScope = 0
	out.Limits.MutationScopeSelectorBytes = 0
	out.Limits.MutationScopeAdvisories = 0
	out.Limits.MutationScopeMinTTLMS = 0
	out.Limits.MutationScopeDefaultTTLMS = 0
	out.Limits.MutationScopeMaxTTLMS = 0
	return out
}

func stripLegacyPersistentCapabilities(out *capability.Catalog) {
	out.PersistentSessionSchemaVersions = nil
	out.SupervisorProtocolVersions = nil
	out.PersistentNonTTY = false
	out.PersistentTTY = false
	out.PersistentContinuity = ""
	out.HostRebootContinuity = false
	delete(out.Features, capability.FeatureNamedSessions)
	out.Limits.PersistentSessions = 0
	out.Limits.PersistentSessionNameBytes = 0
	out.Limits.PersistentSessionInspectRows = 0
	out.Limits.PersistentSessionInspectDefaultRows = 0
	out.Limits.PersistentInputRecords = 0
	out.Limits.PersistentInputRecordMetadataBytes = 0
	out.Limits.PersistentKillRecords = 0
	out.Limits.PersistentRecoverySpoolBytes = 0
	out.Limits.PersistentQueuedInputBytes = 0
	out.Limits.PersistentReattachHandshakeTimeoutMS = 0
	out.Limits.PersistentStartupReattachConcurrency = 0
	out.Limits.PersistentStartupReattachBudgetMS = 0
}

func successV2(action string, out bridge.Response) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 2, "ok": true, "action": action}
	summary := action
	switch action {
	case "start", "poll":
		var failed *mcpgo.CallToolResult
		summary, failed = executionSuccessV2(action, out, body)
		if failed != nil {
			return failed
		}
	case "read_output":
		var failed *mcpgo.CallToolResult
		summary, failed = outputViewSuccessV2(action, out, body)
		if failed != nil {
			return failed
		}
	case "write", "kill":
		body["view"] = controlView(out.View)
		summary = fmt.Sprintf("%s session %s: %s", action, out.View.SessionID, out.View.State)
	case "inspect.workspace", "inspect.activity", "inspect.sessions", "inspect.project", "inspect.readiness", "inspect.code", "inspect.structured", "inspect.telemetry", "inspect.evidence", "inspect.environment", "inspect.process", "repro.create", "inspect.repro", "inspect.events", "inspect.server", "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		var failed *mcpgo.CallToolResult
		summary, failed = inspectionSuccessV2(action, out, body)
		if failed != nil {
			return failed
		}
	}
	return toolSuccess(summary, body)
}

func executionSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.Result == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "structured result missing", false)
	}
	body["result"] = out.Result
	return fmt.Sprintf("%s session %s: %s", action, out.Result.Operation.SessionID, out.Result.Operation.State), nil
}

func outputViewSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.OutputView == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "output view missing", false)
	}
	body["output_view"] = out.OutputView
	return "read_output: " + string(out.OutputView.SelectorKind), nil
}

func inspectionSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	switch action {
	case "inspect.workspace":
		if out.Workspace == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "workspace inspection missing", false)
		}
		body["workspace"] = out.Workspace
		return "inspect.workspace: " + string(out.Workspace.ID), nil
	case "inspect.activity":
		return activitySuccessV2(action, out, body)
	case "inspect.sessions":
		return sessionInspectSuccessV2(action, out, body)
	case "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		return mutationScopeSuccessV2(action, out, body)
	case "inspect.project", "inspect.readiness":
		return projectSuccessV2(action, out, body)
	case "inspect.code":
		if out.CodeResult == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "code inspection missing", false)
		}
		body["code"] = out.CodeResult
		return "inspect.code: " + string(out.CodeResult.Status), nil
	case "inspect.structured":
		if out.Structured == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "structured inspection missing", false)
		}
		body["structured"] = out.Structured
		return "inspect.structured: " + string(out.Structured.Status), nil
	case "inspect.environment":
		if out.Environment == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "environment inspection missing", false)
		}
		body["environment"] = out.Environment
		return "inspect.environment: " + string(out.Environment.Quality), nil
	case "inspect.process":
		if out.Process == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "process inspection missing", false)
		}
		body["process"] = out.Process
		return "inspect.process: " + string(out.Process.Quality), nil
	case "inspect.evidence":
		if out.Evidence == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "evidence inspection missing", false)
		}
		body["evidence"] = out.Evidence
		return "inspect.evidence: " + string(out.Evidence.Status), nil
	case "inspect.telemetry":
		if out.Telemetry == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "telemetry inspection missing", false)
		}
		body["telemetry"] = out.Telemetry
		return fmt.Sprintf("inspect.telemetry: %d sample(s)", out.Telemetry.SamplesReturned), nil
	case "repro.create":
		if out.Capsule == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "repro capsule missing", false)
		}
		body["capsule"] = out.Capsule
		return "repro.create: " + out.Capsule.ReproID, nil
	case "inspect.repro":
		if out.Repro == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "repro inspection missing", false)
		}
		body["repro"] = out.Repro
		return "inspect.repro: " + out.Repro.Capsule.ReproID, nil
	case "inspect.events":
		if out.Events == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "event inspection missing", false)
		}
		body["events"] = out.Events
		return fmt.Sprintf("inspect.events: %d event(s)", len(out.Events.Events)), nil
	case "inspect.server":
		if out.Server == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "server catalog missing", false)
		}
		body["server"] = out.Server
		return "inspect.server: capabilities", nil
	default:
		return action, nil
	}
}

func sessionInspectSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.Sessions == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "session inspection missing", false)
	}
	body["sessions"] = out.Sessions
	return fmt.Sprintf("inspect.sessions: %d session(s)", len(out.Sessions.Sessions)), nil
}

func activitySuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.Activity == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "activity inspection missing", false)
	}
	body["activity"] = out.Activity
	if out.ActivityMutationScopes != nil {
		body["active_mutation_scopes"] = out.ActivityMutationScopes.ActiveScopes
		body["mutation_scope_advisories"] = out.ActivityMutationScopes.Advisories
		body["mutation_scopes_truncated"] = out.ActivityMutationScopes.ScopesTruncated
		body["mutation_scope_advisories_truncated"] = out.ActivityMutationScopes.AdvisoriesTruncated
	}
	return "inspect.activity: " + string(out.Activity.ID), nil
}

func mutationScopeSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if action == "inspect.mutation_scopes" {
		if out.MutationScopes == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "mutation scope inspection missing", false)
		}
		body["mutation_scopes"] = out.MutationScopes
		return fmt.Sprintf("inspect.mutation_scopes: %d active scope(s), %d advisory(s)", out.MutationScopes.ActiveCount, out.MutationScopes.AdvisoryCount), nil
	}
	if out.Mutation == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "mutation scope result missing", false)
	}
	body["mutation"] = out.Mutation
	return fmt.Sprintf("%s: %s", action, out.Mutation.Receipt.Result), nil
}

func projectSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	switch action {
	case "inspect.project":
		if out.Project == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "project inspection missing", false)
		}
		body["project"] = out.Project
		return "inspect.project: " + string(out.Project.Status), nil
	case "inspect.readiness":
		if out.Readiness == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "project readiness missing", false)
		}
		body["readiness"] = out.Readiness
		return "inspect.readiness: " + string(out.Readiness.State), nil
	default:
		return "", toolErrorV2(action, "invalid_daemon_response", "project response action invalid", false)
	}
}

func controlView(view app.View) map[string]any {
	body := map[string]any{
		"session_id": view.SessionID, "state": view.State, "outcome": view.Outcome,
		"cursor": view.Cursor, "next_cursor": view.NextCursor, "truncated": view.Truncated,
		"accepted_input_bytes": view.AcceptedInputBytes, "next_input_offset": view.NextInputOffset,
		"eof_queued": view.EOFQueued, "kill_id": view.KillID, "signal": view.Signal,
	}
	if view.OperationID != "" {
		body["operation_id"] = view.OperationID
	}
	if view.Output != "" {
		body["output"] = view.Output
	}
	if view.Receipt != nil {
		body["receipt"] = view.Receipt
	}
	return body
}

func toolSuccess(summary string, body map[string]any) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{&mcpgo.TextContent{Text: summary}}, StructuredContent: body}
}

func versionedToolError(version int, action, code, message string, retryable bool) *mcpgo.CallToolResult {
	if version == 2 {
		return toolErrorV2(action, code, message, retryable)
	}
	return toolError(code, message, retryable)
}

func toolError(code, message string, retryable bool) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": code, "message": message, "retryable": retryable}}
	return &mcpgo.CallToolResult{IsError: true, Content: []mcpgo.Content{&mcpgo.TextContent{Text: code + ": " + message}}, StructuredContent: body}
}

func toolErrorV2(action, code, message string, retryable bool) *mcpgo.CallToolResult {
	body := map[string]any{"schema_version": 2, "ok": false, "action": action, "error": map[string]any{"code": code, "message": message, "retryable": retryable}}
	return &mcpgo.CallToolResult{IsError: true, Content: []mcpgo.Content{&mcpgo.TextContent{Text: code + ": " + message}}, StructuredContent: body}
}

func cloneMCPStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
