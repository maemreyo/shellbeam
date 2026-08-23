package ipc

import (
	"fmt"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateStartResourceHermeticV2(v RequestV2) error {
	if v.ResourceLimits != nil {
		if err := v.ResourceLimits.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "limits"}, err)
		}
	}
	if v.Hermetic != nil {
		if err := v.Hermetic.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "hermetic"}, err)
		}
		if v.TTY || v.Persistent || (v.StdinMode != "" && v.StdinMode != operation.StdinModeClosed) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "hermetic"}, fmt.Errorf("hermetic v1 requires non-tty, non-persistent, closed stdin"))
		}
	}
	return nil
}

func validateStartRequestV2(v RequestV2) error {
	if err := validateDelegatedStartRequestV2(v); err != nil {
		return err
	}
	if err := validateStartResourceHermeticV2(v); err != nil {
		return err
	}
	if v.OperationID == "" {
		return failure.New(failure.InvalidInput, map[string]string{"reason": "missing_start_field"}, fmt.Errorf("missing start field"))
	}
	if _, err := operation.ParseID(v.OperationID); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "operation_id"}, err)
	}
	typed := v.ProjectCommandID != "" || v.Params != nil
	if typed {
		if v.ProjectCommandID == "" || v.WorkspaceID == "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, fmt.Errorf("typed project command requires workspace and command id"))
		}
		if v.Command != "" || len(v.Argv) != 0 || v.CWD != "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, fmt.Errorf("typed project command conflicts with raw execution fields"))
		}
		intent := operation.TypedRequestIntent{WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, Params: v.Params, TTY: v.TTY, TimeoutMS: v.TimeoutMS, Persistent: v.Persistent, SessionMode: v.SessionMode, SessionName: v.SessionName, StdinMode: v.StdinMode, TimeoutMode: v.TimeoutMode, TraceMode: v.TraceMode, ResourceLimits: v.ResourceLimits.Clone(), Hermetic: v.Hermetic, VerificationAttempt: v.VerificationAttempt}
		if err := intent.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "project_command_id"}, err)
		}
	} else {
		if _, err := (operation.Intent{Command: v.Command, Argv: v.Argv}).ExecutionMode(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "command"}, err)
		}
		address := workspace.Address{WorkspaceID: workspace.WorkspaceID(v.WorkspaceID), CWD: v.CWD}
		if err := address.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "cwd"}, err)
		}
	}
	if v.VerificationAttempt != nil {
		if err := v.VerificationAttempt.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "verification_attempt"}, err)
		}
		if !typed && v.Evidence == nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "verification_attempt"}, fmt.Errorf("raw verification attempt requires evidence contract"))
		}
	}
	if v.Evidence != nil {
		if _, err := v.Evidence.Normalize(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, err)
		}
		if typed {
			return failure.New(failure.InvalidInput, map[string]string{"field": "evidence"}, fmt.Errorf("typed project commands use frozen manifest evidence"))
		}
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "intent"}, err)
		}
	}
	if v.WorkspaceHint != nil {
		if err := v.WorkspaceHint.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_hint"}, err)
		}
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "structured_adapter"}, fmt.Errorf("invalid structured adapter"))
	}
	if v.SessionName != "" && !v.Persistent && v.SessionMode != delegated.ModeDelegatedInteractive {
		return failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, fmt.Errorf("session_name requires persistent or delegated interactive"))
	}
	if _, err := trace.NormalizeMode(v.TraceMode); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	return nil
}
