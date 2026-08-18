package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	mutationcore "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type input struct {
	Action              string                            `json:"action"`
	CheckpointCreateID  string                            `json:"checkpoint_create_id,omitempty"`
	RestoreID           string                            `json:"restore_id,omitempty"`
	CheckpointID        string                            `json:"checkpoint_id,omitempty"`
	OperationID         string                            `json:"operation_id,omitempty"`
	WorkspaceID         string                            `json:"workspace_id,omitempty"`
	ActivityID          string                            `json:"activity_id,omitempty"`
	CodeQuery           *codeintel.Query                  `json:"code_query,omitempty"`
	WorkspaceHint       *workspace.Hint                   `json:"workspace_hint,omitempty"`
	StructuredAdapter   string                            `json:"structured_adapter,omitempty"`
	ProjectCommandID    string                            `json:"project_command_id,omitempty"`
	Params              map[string]string                 `json:"params,omitempty"`
	Command             string                            `json:"command,omitempty"`
	Argv                []string                          `json:"argv,omitempty"`
	Intent              *operation.DeclaredIntent         `json:"intent,omitempty"`
	Evidence            *coreevidence.Contract            `json:"evidence,omitempty"`
	Freshness           environmentcore.Freshness         `json:"freshness,omitempty"`
	Execution           *environmentcore.ExecutionContext `json:"execution,omitempty"`
	ProcessTarget       *processcore.Target               `json:"process_target,omitempty"`
	IncludePorts        bool                              `json:"include_ports,omitempty"`
	CWD                 string                            `json:"cwd,omitempty"`
	TTY                 bool                              `json:"tty,omitempty"`
	Persistent          bool                              `json:"persistent,omitempty"`
	SessionName         string                            `json:"session_name,omitempty"`
	YieldMS             int64                             `json:"yield_time_ms,omitempty"`
	TimeoutMS           int64                             `json:"timeout_ms,omitempty"`
	StdinMode           operation.StdinMode               `json:"stdin_mode,omitempty"`
	TimeoutMode         operation.TimeoutMode             `json:"timeout_mode,omitempty"`
	TraceMode           trace.Mode                        `json:"trace_mode,omitempty"`
	ResourceLimits      *operation.ResourceLimits         `json:"limits,omitempty"`
	Hermetic            *hermeticcore.Request             `json:"hermetic,omitempty"`
	MaxOutputBytes      int                               `json:"max_output_bytes,omitempty"`
	SessionID           string                            `json:"session_id,omitempty"`
	Selector            *outputview.Selector              `json:"selector,omitempty"`
	Cursor              int64                             `json:"cursor,omitempty"`
	InputOffset         int64                             `json:"input_offset,omitempty"`
	Chars               string                            `json:"chars,omitempty"`
	EOF                 bool                              `json:"eof,omitempty"`
	KillID              string                            `json:"kill_id,omitempty"`
	Signal              string                            `json:"signal,omitempty"`
	Target              *observationcore.Target           `json:"target,omitempty"`
	AfterEventCursor    string                            `json:"after_event_cursor,omitempty"`
	MaxEvents           int                               `json:"max_events,omitempty"`
	RecordKind          structuredcore.RecordKind         `json:"record_kind,omitempty"`
	Severity            structuredcore.Severity           `json:"severity,omitempty"`
	Path                string                            `json:"path,omitempty"`
	TestStatus          structuredcore.TestStatus         `json:"test_status,omitempty"`
	State               string                            `json:"state,omitempty"`
	PersistentOnly      *bool                             `json:"persistent_only,omitempty"`
	Continuation        string                            `json:"continuation,omitempty"`
	MaxRecords          int                               `json:"max_records,omitempty"`
	EvidenceID          string                            `json:"evidence_id,omitempty"`
	VerificationKind    coreevidence.VerificationKind     `json:"verification_kind,omitempty"`
	EvidenceResult      coreevidence.Result               `json:"result,omitempty"`
	RevalidateArtifacts bool                              `json:"revalidate_artifacts,omitempty"`
	MaxSamples          int                               `json:"max_samples,omitempty"`
	MaxResources        int                               `json:"max_resources,omitempty"`
	ReproCreateID       string                            `json:"repro_create_id,omitempty"`
	CapturePolicy       *reprocore.CapturePolicy          `json:"capture_policy,omitempty"`
	ReproID             string                            `json:"repro_id,omitempty"`
	MutationID          string                            `json:"mutation_id,omitempty"`
	ScopeID             string                            `json:"scope_id,omitempty"`
	Mode                mutationcore.Mode                 `json:"mode,omitempty"`
	Paths               []string                          `json:"paths,omitempty"`
	TTLMS               int64                             `json:"ttl_ms,omitempty"`
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func hasField(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}

func protocolGeneration(version string) int {
	if version >= modernMCP {
		return 2
	}
	return 1
}

func validateForVersion(version int, v input, raw []byte) error {
	if version == 2 {
		if err := validateV2FieldSet(v.Action, raw); err != nil {
			return err
		}
		return validateV2(v)
	}
	if v.Action == "inspect.trace" || hasField(raw, "trace_mode") || hasField(raw, "max_resources") {
		return fmt.Errorf("input tracing requires modern protocol")
	}
	if hasField(raw, "limits") {
		return fmt.Errorf("resource limits require modern protocol")
	}
	if hasField(raw, "hermetic") {
		return fmt.Errorf("hermetic execution requires modern protocol")
	}
	if v.Action == "inspect.sessions" || hasField(raw, "persistent") || hasField(raw, "session_name") || hasField(raw, "persistent_only") {
		return fmt.Errorf("persistent sessions require modern protocol")
	}
	if v.Action == "inspect.server" {
		if err := validateV2FieldSet(v.Action, raw); err != nil {
			return err
		}
	}
	return validateV1(v)
}

func validateV2(v input) error {
	if v.Action == "start" && v.ResourceLimits != nil {
		if err := v.ResourceLimits.Validate(); err != nil {
			return err
		}
	}
	if v.Action == "start" {
		if err := validateHermeticStartInput(v); err != nil {
			return err
		}
	}
	switch v.Action {
	case "read_media":
		return validateMediaInput(v)
	case "read_output":
		if v.Selector == nil {
			return fmt.Errorf("read_output requires selector")
		}
		return (outputview.Request{SessionID: v.SessionID, Selector: *v.Selector, Continuation: v.Continuation}).Validate()
	case "checkpoint_create", "checkpoint_restore", "checkpoint_inspect":
		return validateCheckpointInput(v)
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		_, err := workspace.ParseWorkspaceID(v.WorkspaceID)
		return err
	case "inspect.activity":
		_, err := activity.ParseID(v.ActivityID)
		return err
	case "inspect.sessions":
		return validateSessionInspectInput(v)
	case "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		return validateMutationScopeInput(v)
	case "inspect.code":
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return err
		}
		if v.ActivityID != "" {
			if _, err := activity.ParseID(v.ActivityID); err != nil {
				return err
			}
		}
		if v.CodeQuery == nil {
			return fmt.Errorf("inspect.code requires code_query")
		}
		return v.CodeQuery.Validate()
	case "inspect.evidence":
		_, err := evidenceapp.NormalizeInspectRequestForTransport(evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{EvidenceID: v.EvidenceID, OperationID: v.OperationID, WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, ActivityID: v.ActivityID, VerificationKind: v.VerificationKind, Result: v.EvidenceResult, RevalidateArtifacts: v.RevalidateArtifacts}, Continuation: v.Continuation, MaxRecords: v.MaxRecords})
		return err
	case "inspect.environment":
		if v.WorkspaceID != "" {
			if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
				return err
			}
		}
		if v.Freshness != "" && v.Freshness != environmentcore.FreshnessCached && v.Freshness != environmentcore.FreshnessRefresh {
			return fmt.Errorf("invalid freshness")
		}
		if v.Execution != nil && ((v.Execution.Mode != "shell" && v.Execution.Mode != "argv") || v.Execution.Identity == "") {
			return fmt.Errorf("invalid execution identity")
		}
		return nil
	case "inspect.process":
		if v.ProcessTarget == nil {
			return fmt.Errorf("inspect.process requires process_target")
		}
		return v.ProcessTarget.Validate()
	case "inspect.structured":
		return (structuredapp.InspectRequest{OperationID: v.OperationID, Filter: structuredapp.RecordFilter{RecordKind: v.RecordKind, Severity: v.Severity, Path: v.Path, TestStatus: v.TestStatus}, Continuation: v.Continuation, MaxRecords: v.MaxRecords}).Validate()
	case "inspect.telemetry", "repro.create", "inspect.repro":
		return validateA4Input(v)
	case "inspect.trace":
		return validateInputTrace(v)
	case "inspect.events":
		return validateEventInspectInput(v)
	}
	if v.Action != "start" {
		return validateV1(v)
	}
	return validateStartV2(v)
}

func validateHermeticStartInput(v input) error {
	if v.Hermetic == nil {
		return nil
	}
	if err := v.Hermetic.Validate(); err != nil {
		return err
	}
	if v.TTY || v.Persistent || (v.StdinMode != "" && v.StdinMode != operation.StdinModeClosed) {
		return fmt.Errorf("hermetic v1 requires non-tty, non-persistent, closed stdin")
	}
	return nil
}

func validateStartV2(v input) error {
	if v.OperationID == "" {
		return fmt.Errorf("start requires operation_id")
	}
	if _, err := operation.ParseID(v.OperationID); err != nil {
		return err
	}
	typed := v.ProjectCommandID != "" || v.Params != nil
	if typed {
		if v.ProjectCommandID == "" || v.WorkspaceID == "" {
			return fmt.Errorf("typed project command requires workspace and command id")
		}
		if v.Command != "" || len(v.Argv) != 0 || v.CWD != "" {
			return fmt.Errorf("typed project command conflicts with raw execution fields")
		}
		if err := (operation.TypedRequestIntent{WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, Params: v.Params, TTY: v.TTY, TimeoutMS: v.TimeoutMS, Hermetic: v.Hermetic}).Validate(); err != nil {
			return err
		}
	} else {
		if _, err := (operation.Intent{Command: v.Command, Argv: v.Argv}).ExecutionMode(); err != nil {
			return err
		}
		address := workspace.Address{WorkspaceID: workspace.WorkspaceID(v.WorkspaceID), CWD: v.CWD}
		if err := address.Validate(); err != nil {
			return err
		}
	}
	if v.Evidence != nil {
		if _, err := v.Evidence.Normalize(); err != nil {
			return err
		}
		if typed {
			return fmt.Errorf("typed project commands use frozen manifest evidence")
		}
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return err
		}
	}
	if v.WorkspaceHint != nil {
		if err := v.WorkspaceHint.Validate(); err != nil {
			return err
		}
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return err
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid structured adapter")
	}
	if v.SessionName != "" {
		if !v.Persistent {
			return fmt.Errorf("session_name requires persistent")
		}
		if err := persistent.ValidateSessionName(v.SessionName); err != nil {
			return err
		}
	}
	if v.Persistent && v.TTY {
		return fmt.Errorf("persistent tty unsupported")
	}
	if err := validateInputTrace(v); err != nil {
		return err
	}
	return validateNonNegative(v)
}

func validateMutationScopeInput(v input) error {
	switch v.Action {
	case "mutation_scope.set":
		return validateMutationScopeSetInput(v)
	case "mutation_scope.release":
		if mutationcore.ValidateMutationID(v.MutationID) != nil || mutationcore.ValidateScopeID(v.ScopeID) != nil {
			return fmt.Errorf("invalid mutation scope release")
		}
	case "inspect.mutation_scopes":
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return err
		}
		if v.ActivityID != "" {
			_, err := activity.ParseID(v.ActivityID)
			return err
		}
	}
	return nil
}

func validateMutationScopeSetInput(v input) error {
	if mutationcore.ValidateMutationID(v.MutationID) != nil || mutationcore.ValidateScopeID(v.ScopeID) != nil {
		return fmt.Errorf("invalid mutation scope id")
	}
	if _, err := activity.ParseID(v.ActivityID); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
		return err
	}
	if v.Mode != mutationcore.ModeRead && v.Mode != mutationcore.ModeMutate {
		return fmt.Errorf("invalid mutation scope mode")
	}
	if _, err := mutationcore.NormalizeSelectors(v.Paths); err != nil {
		return err
	}
	if v.TTLMS != 0 && (v.TTLMS < mutationcore.MinTTL.Milliseconds() || v.TTLMS > mutationcore.MaxTTL.Milliseconds()) {
		return fmt.Errorf("invalid mutation scope ttl")
	}
	_ = mutationapp.SetRequest{}
	return nil
}

func validateA4Input(v input) error {
	switch v.Action {
	case "inspect.telemetry":
		if _, err := operation.ParseID(v.OperationID); err != nil {
			return err
		}
		if v.MaxSamples < 1 || v.MaxSamples > telemetryapp.MaxInspectSamples {
			return fmt.Errorf("invalid max_samples")
		}
	case "repro.create":
		policy := reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}
		if v.CapturePolicy != nil {
			policy = *v.CapturePolicy
		}
		return (reprocore.CreateRequest{CreateID: v.ReproCreateID, OperationID: v.OperationID, Policy: policy}).Validate()
	case "inspect.repro":
		if !validReproIDInput(v.ReproID) {
			return fmt.Errorf("invalid repro_id")
		}
	}
	return nil
}

func validateV1(v input) error {
	switch v.Action {
	case "start":
		if v.ProjectCommandID != "" || v.Params != nil {
			return fmt.Errorf("typed project commands require modern protocol")
		}
		if v.OperationID == "" || v.Command == "" || len(v.CWD) == 0 || v.CWD[0] != '/' {
			return fmt.Errorf("start requires operation_id, command, and absolute cwd")
		}
		if _, err := operation.ParseID(v.OperationID); err != nil {
			return err
		}
		if v.SessionID != "" || v.Chars != "" || v.EOF || v.KillID != "" {
			return fmt.Errorf("cross-action field")
		}
	case "poll":
		if v.SessionID == "" || v.OperationID != "" || v.Command != "" || v.Chars != "" || v.KillID != "" {
			return fmt.Errorf("invalid poll fields")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
	case "write":
		if v.SessionID == "" || v.InputOffset < 0 || (v.Chars == "") == (!v.EOF) {
			return fmt.Errorf("write requires exactly chars or eof")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
		if v.OperationID != "" || v.Command != "" || v.KillID != "" {
			return fmt.Errorf("cross-action field")
		}
	case "inspect.server":
		return validateNonNegative(v)
	case "kill":
		if v.SessionID == "" || v.KillID == "" {
			return fmt.Errorf("kill requires session_id and kill_id")
		}
		if _, err := operation.ParseSessionID(v.SessionID); err != nil {
			return err
		}
		if _, err := operation.ParseID(v.KillID); err != nil {
			return err
		}
		if v.Signal != "" && v.Signal != "INT" && v.Signal != "TERM" && v.Signal != "KILL" {
			return fmt.Errorf("invalid signal")
		}
		if v.OperationID != "" || v.Command != "" || v.Chars != "" || v.EOF {
			return fmt.Errorf("cross-action field")
		}
	default:
		return fmt.Errorf("unknown action")
	}
	return validateNonNegative(v)
}

func validateNonNegative(v input) error {
	if v.YieldMS < 0 || v.TimeoutMS < 0 || v.MaxOutputBytes < 0 || v.Cursor < 0 || v.InputOffset < 0 {
		return fmt.Errorf("negative value")
	}
	return nil
}

// validateEventInspectInput follows the same shape as the other per-action
// validators: the switch names the action, a helper owns its rules.
func validateEventInspectInput(v input) error {
	if v.Target == nil {
		return fmt.Errorf("inspect.events requires target")
	}
	if err := v.Target.Validate(); err != nil {
		return err
	}
	if v.MaxEvents < 1 || v.MaxEvents > observationapp.MaxInspectEvents {
		return fmt.Errorf("invalid max_events")
	}
	if v.AfterEventCursor != "" && (!strings.HasPrefix(v.AfterEventCursor, observationapp.EventCursorPrefix) || len(v.AfterEventCursor) > observationapp.MaxEventCursorBytes) {
		return fmt.Errorf("invalid event cursor")
	}
	return nil
}

func validateV2FieldSet(action string, raw []byte) error {
	allowed := map[string]bool{"action": true}
	for _, field := range v2ActionFields(action) {
		allowed[field] = true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("cross-action field %q", field)
		}
	}
	return nil
}

func isDeferredAction(string) bool { return false }
