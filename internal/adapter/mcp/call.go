package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
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
		request.Start = app.StartRequest{OperationID: in.OperationID, ActivityID: in.ActivityID, WorkspaceID: in.WorkspaceID, WorkspaceHint: in.WorkspaceHint, StructuredAdapter: in.StructuredAdapter, ProjectCommandID: in.ProjectCommandID, Params: cloneMCPStringMap(in.Params), Command: in.Command, Argv: append([]string(nil), in.Argv...), Intent: in.Intent, Evidence: in.Evidence, CWD: in.CWD, TTY: in.TTY, YieldMS: yieldMS, TimeoutMS: in.TimeoutMS, MaxOutputBytes: maxOutput}
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
	return out
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
	case "inspect.workspace", "inspect.activity", "inspect.project", "inspect.readiness", "inspect.code", "inspect.structured", "inspect.telemetry", "inspect.evidence", "repro.create", "inspect.repro", "inspect.events", "inspect.server":
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
		if out.Activity == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "activity inspection missing", false)
		}
		body["activity"] = out.Activity
		return "inspect.activity: " + string(out.Activity.ID), nil
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
