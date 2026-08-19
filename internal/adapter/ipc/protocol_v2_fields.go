package ipc

import (
	"encoding/json"
	"fmt"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func actionFieldsV2(action string) []string {
	switch action {
	case "capabilities.negotiate":
		return []string{"consumer_media"}
	case "read_media":
		return []string{"consumer_media", "media_contract_fingerprint", "media"}
	case "start":
		return []string{"operation_id", "workspace_id", "activity_id", "workspace_hint", "structured_adapter", "project_command_id", "params", "command", "argv", "intent", "evidence", "cwd", "tty", "persistent", "session_mode", "session_name", "timeout_ms", "stdin_mode", "timeout_mode", "trace_mode", "limits", "yield_time_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield_time_ms", "max_output_bytes"}
	case "handoff.request":
		return []string{"handoff_id", "session_id", "reason", "privacy", "completion"}
	case "handoff.wait":
		return []string{"handoff_id", "yield_time_ms"}
	case "handoff.abort", "inspect.handoff":
		return []string{"handoff_id"}
	case "read_output":
		return []string{"session_id", "selector", "continuation"}
	case "write":
		return []string{"session_id", "authority_epoch", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "authority_epoch", "kill_id", "signal"}
	case "checkpoint_create":
		return []string{"checkpoint_create_id", "workspace_id", "activity_id", "paths"}
	case "checkpoint_restore":
		return []string{"restore_id", "checkpoint_id", "paths"}
	case "checkpoint_inspect":
		return []string{"checkpoint_id"}
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		return []string{"workspace_id"}
	case "inspect.activity":
		return []string{"activity_id"}
	case "inspect.sessions":
		return []string{"session_name", "activity_id", "workspace_id", "state", "persistent_only", "continuation", "max_records"}
	case "mutation_scope.set":
		return []string{"mutation_id", "scope_id", "activity_id", "workspace_id", "mode", "paths", "ttl_ms"}
	case "mutation_scope.release":
		return []string{"mutation_id", "scope_id"}
	case "inspect.mutation_scopes":
		return []string{"workspace_id", "activity_id"}
	case "inspect.events":
		return []string{"target", "after_event_cursor", "max_events"}
	case "inspect.structured":
		return []string{"operation_id", "record_kind", "severity", "path", "test_status", "continuation", "max_records"}
	case "inspect.telemetry":
		return []string{"operation_id", "max_samples"}
	case "inspect.trace":
		return []string{"operation_id", "max_resources"}
	case "inspect.evidence":
		return []string{"evidence_id", "operation_id", "workspace_id", "project_command_id", "activity_id", "verification_kind", "result", "revalidate_artifacts", "continuation", "max_records"}
	case "inspect.environment":
		return []string{"workspace_id", "freshness", "execution"}
	case "inspect.process":
		return []string{"process_target", "include_ports"}
	case "repro.create":
		return []string{"repro_create_id", "operation_id", "capture_policy"}
	case "inspect.repro":
		return []string{"repro_id"}
	case "inspect.code":
		return []string{"workspace_id", "activity_id", "code_query"}
	default:
		return nil
	}
}
func hasV2Field(data []byte, field string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}
func validateDelegatedRequestShapeV2(mode string, tty, persistent bool) error {
	if mode == "" {
		return nil
	}
	if err := delegated.ValidateMode(mode); err != nil {
		return err
	}
	if mode == delegated.ModeDelegatedInteractive && (tty || persistent) {
		return fmt.Errorf("delegated interactive conflicts with legacy tty/persistent fields")
	}
	return nil
}
func validateDelegatedStartRequestV2(v RequestV2) error {
	if err := validateDelegatedRequestShapeV2(v.SessionMode, v.TTY, v.Persistent); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "session_mode"}, err)
	}
	if err := v.StdinMode.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "stdin_mode"}, err)
	}
	if err := v.TimeoutMode.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "timeout_mode"}, err)
	}
	if v.SessionMode != delegated.ModeDelegatedInteractive {
		return nil
	}
	if v.Evidence != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, fmt.Errorf("delegated lifecycle is not ordinary verification evidence"))
	}
	if v.ResourceLimits != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "limits"}, fmt.Errorf("delegated resource limits are not qualified in H1"))
	}
	if v.StdinMode == operation.StdinModeClosed {
		return failure.New(failure.InvalidInput, map[string]string{"field": "stdin_mode"}, fmt.Errorf("delegated interactive requires stream stdin in H1"))
	}
	if v.TimeoutMS != 0 || v.TimeoutMode == operation.TimeoutModeFinite || v.TimeoutMode == operation.TimeoutModeDefault {
		return failure.New(failure.InvalidInput, map[string]string{"field": "timeout_mode"}, fmt.Errorf("bounded delegated timeout is not qualified in H1"))
	}
	mode, err := trace.NormalizeMode(v.TraceMode)
	if err != nil || mode != trace.ModeOff {
		return failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, fmt.Errorf("delegated input trace is not qualified in H1"))
	}
	return nil
}
