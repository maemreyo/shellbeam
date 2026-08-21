package ipc

func actionFieldsV2(action string) []string {
	switch action {
	case "capabilities.negotiate":
		return []string{"consumer_media"}
	case "read_media":
		return []string{"consumer_media", "media_contract_fingerprint", "media"}
	case "start":
		return []string{"operation_id", "experiment_id", "workspace_id", "activity_id", "workspace_hint", "structured_adapter", "project_command_id", "params", "command", "argv", "intent", "evidence", "verification_attempt", "cwd", "tty", "persistent", "session_name", "timeout_ms", "stdin_mode", "timeout_mode", "trace_mode", "limits", "hermetic", "yield_time_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield_time_ms", "max_output_bytes"}
	case "read_output":
		return []string{"session_id", "selector", "continuation"}
	case "write":
		return []string{"session_id", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "kill_id", "signal"}
	case "checkpoint_create":
		return []string{"checkpoint_create_id", "workspace_id", "activity_id", "paths"}
	case "checkpoint_restore":
		return []string{"restore_id", "checkpoint_id", "paths"}
	case "checkpoint_inspect":
		return []string{"checkpoint_id"}
	case "inspect.verification":
		return []string{"workspace_id", "activity_id", "phase"}
	case "verification.policy.preview":
		return []string{"workspace_id", "profile"}
	case "verification.policy.activate":
		return []string{"workspace_id", "activation_id", "proposed_policy_digest", "expected_previous_policy_digest", "proposal_generation", "authority", "actor"}
	case "verification.waiver.set":
		return []string{"workspace_id", "waiver_id", "policy_digest", "rule_id", "phase", "generation", "checkpoint_id", "authority", "actor", "reason", "expires_at", "expires_phase"}
	case "verification.waiver.revoke":
		return []string{"workspace_id", "waiver_id", "authority", "actor"}
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
	case "decision.policy.snapshot", "decision.policy.activate", "decision.create", "decision.inspect", "decision.evaluate", "decision.close_unresolved", "decision.candidate.create", "decision.candidate.revise", "decision.experiment.define", "decision.prediction.bind", "decision.experiment.seal", "decision.experiment.close", "decision.experiment.abort", "decision.assessment.record", "decision.selection.propose", "decision.override.create", "decision.selection.commit", "decision.authority.materialize":
		return []string{"decision", "workspace_id"}
	default:
		return nil
	}
}
