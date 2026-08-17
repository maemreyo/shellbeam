package mcp

import (
	"context"
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func call(ctx context.Context, h *bridge.Handler, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	version := protocolGeneration(req.ProtocolVersion())
	var in input
	var err error
	if version == 2 {
		in, err = decodeInputV2(req.Params.Arguments)
	} else {
		in, err = decodeInput(req.Params.Arguments)
	}
	if err != nil {
		return versionedToolError(version, "", "invalid_request", "invalid request", false), nil
	}
	if version == 2 && isDeferredAction(in.Action) {
		return toolErrorV2(in.Action, "feature_unavailable", "feature unavailable", false), nil
	}
	if version == 2 && in.Action == "read_media" && !mediaCatalogAvailable(h.EffectiveCatalog()) {
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
		if in.Action == "read_media" {
			return mediaSuccessV2(in, out), nil
		}
		return successV2(in.Action, out), nil
	}
	return successV1(in.Action, out), nil
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
