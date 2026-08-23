package mcp

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func preflightV2Input(raw []byte) (string, map[string]string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return "", map[string]string{"reason": "invalid_json"}
	}
	rawAction, ok := fields["action"]
	if !ok {
		for field := range fields {
			if strings.EqualFold(field, "action") {
				return "", map[string]string{"field": field, "reason": "unknown_field"}
			}
		}
		return "", map[string]string{"field": "action", "reason": "missing_field"}
	}
	var action string
	if err := json.Unmarshal(rawAction, &action); err != nil || action == "" {
		return "", map[string]string{"field": "action", "reason": "invalid_value"}
	}
	allowed := map[string]bool{"action": true}
	for _, field := range v2ActionFields(action) {
		allowed[field] = true
	}
	if action != "inspect.server" && len(allowed) == 1 {
		return action, map[string]string{"field": "action", "reason": "invalid_value"}
	}
	unknown := make([]string, 0)
	for field := range fields {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) != 0 {
		sort.Strings(unknown)
		return action, map[string]string{"field": unknown[0], "reason": "unknown_field"}
	}
	return action, nil
}

func classifyV2DecodeFailure(raw []byte) map[string]string {
	var probe input
	if err := json.Unmarshal(raw, &probe); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			details := map[string]string{"reason": "invalid_value"}
			if field := jsonFieldForInputTypeError(typeErr.Field); field != "" {
				details["field"] = field
			}
			return details
		}
		return map[string]string{"reason": "invalid_json"}
	}
	return map[string]string{"reason": "invalid_json"}
}

func jsonFieldForInputTypeError(field string) string {
	if field == "" {
		return ""
	}
	first := strings.Split(field, ".")[0]
	// encoding/json may report either JSON names or Go field names. Keep the
	// public surface bounded to names already present in the input contract.
	for _, candidate := range allV2InputFields() {
		if candidate == first || strings.EqualFold(strings.ReplaceAll(candidate, "_", ""), first) {
			return candidate
		}
	}
	return ""
}

func allV2InputFields() []string {
	return []string{
		"action", "activity_id", "after_event_cursor", "argv", "capture_policy", "chars", "checkpoint_create_id", "checkpoint_id",
		"code_query", "command", "continuation", "cursor", "cwd", "eof", "evidence", "evidence_id", "execution", "freshness", "hermetic",
		"include_ports", "input_offset", "intent", "kill_id", "limits", "max_events", "max_output_bytes", "max_records", "max_resources", "max_samples",
		"decision", "experiment_id", "mode", "mutation_id", "operation_id", "params", "path", "paths", "persistent", "persistent_only", "process_target", "project_command_id",
		"record_kind", "repro_create_id", "repro_id", "restore_id", "result", "revalidate_artifacts", "scope_id", "selector", "session_id", "session_name",
		"severity", "signal", "state", "structured_adapter", "target", "test_status", "timeout_ms", "trace_mode", "ttl_ms", "tty", "verification_kind",
		"workspace_hint", "workspace_id", "yield_time_ms",
	}
}

func classifyV2ValidationFailure(action string, err error) map[string]string {
	message := err.Error()
	missing := map[string]string{
		"start requires operation_id":             "operation_id",
		"read_output requires selector":           "selector",
		"inspect.process requires process_target": "process_target",
		"inspect.events requires target":          "target",
		"inspect.code requires code_query":        "code_query",
	}
	if field := missing[message]; field != "" {
		return map[string]string{"field": field, "reason": "missing_field"}
	}
	return map[string]string{"reason": "invalid_value"}
}

func invalidInputV2(action string, details map[string]string) *mcpgo.CallToolResult {
	public := failure.Public(failure.New(failure.InvalidInput, details, nil))
	return versionedToolErrorDetails(2, action, string(public.Code), public.Message, public.Retryable, public.Details)
}
