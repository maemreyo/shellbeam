package ipc

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/app/outputview"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateActionRequestV2(v RequestV2) error {
	switch v.Action {
	case "start":
		return validateStartRequestV2(v)
	case "context.exec":
		req := contextcore.Request{ContextExecID: v.ContextExecID, SessionID: v.SessionID, AuthorityEpoch: v.AuthorityEpoch, Argv: append([]string(nil), v.Argv...), TimeoutMS: v.TimeoutMS, MaxOutputBytes: int64(v.MaxOutputBytes)}
		if err := req.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "context_exec"}, err)
		}
	case "poll":
		if v.SessionID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "session_id"}, fmt.Errorf("missing session id"))
		}
	case "read_output":
		if v.Selector == nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "selector"}, fmt.Errorf("output selector missing"))
		}
		if err := (outputview.Request{SessionID: v.SessionID, Selector: *v.Selector, Continuation: v.Continuation}).Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "read_output"}, err)
		}
	case "handoff.request", "handoff.wait", "handoff.abort", "inspect.handoff":
		return validateHandoffRequestV2(v)
	case "write":
		if v.SessionID == "" || (v.Chars == "" && !v.EOF) || (v.Chars != "" && v.EOF) {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_write"}, fmt.Errorf("invalid write request"))
		}
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
	case "inspect.activity":
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
		}
	case "checkpoint_create", "checkpoint_restore", "checkpoint_inspect":
		return validateCheckpointRequestV2(v)
	case "inspect.sessions":
		return validateSessionInspectV2(v)
	case "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		return validateMutationScopeRequestV2(v)
	case "inspect.structured":
		return validateStructuredInspectV2(v)
	case "inspect.evidence":
		return validateEvidenceInspectV2(v)
	case "inspect.environment":
		return validateEnvironmentInspectV2(v)
	case "inspect.process":
		return validateProcessInspectV2(v)
	case "inspect.telemetry", "repro.create", "inspect.repro":
		return validateA4RequestV2(v)
	case "inspect.trace":
		return validateInputTraceRequestV2(v)
	case "inspect.code":
		return validateCodeInspectV2(v)
	case "inspect.events":
		return validateEventInspectV2(v)
	case "kill":
		if v.SessionID == "" || v.KillID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"reason": "missing_kill_field"}, fmt.Errorf("missing kill field"))
		}
	}
	return nil
}
