package ipc

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	mutationscopeapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/media"
	mutationscopecore "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const ipcV2 = 2

type RequestV2 struct {
	ConsumerMedia            *capability.MediaSupport          `json:"consumer_media,omitempty"`
	MediaContractFingerprint string                            `json:"media_contract_fingerprint,omitempty"`
	Media                    *app.MediaRequest                 `json:"media,omitempty"`
	IPVersion                int                               `json:"ipc_version"`
	Kind                     string                            `json:"kind"`
	RequestID                string                            `json:"request_id"`
	Action                   string                            `json:"action"`
	CheckpointCreateID       string                            `json:"checkpoint_create_id,omitempty"`
	RestoreID                string                            `json:"restore_id,omitempty"`
	CheckpointID             string                            `json:"checkpoint_id,omitempty"`
	OperationID              string                            `json:"operation_id,omitempty"`
	WorkspaceID              string                            `json:"workspace_id,omitempty"`
	ActivityID               string                            `json:"activity_id,omitempty"`
	CodeQuery                *codeintel.Query                  `json:"code_query,omitempty"`
	WorkspaceHint            *workspace.Hint                   `json:"workspace_hint,omitempty"`
	StructuredAdapter        string                            `json:"structured_adapter,omitempty"`
	ProjectCommandID         string                            `json:"project_command_id,omitempty"`
	Params                   map[string]string                 `json:"params,omitempty"`
	Command                  string                            `json:"command,omitempty"`
	Argv                     []string                          `json:"argv,omitempty"`
	Intent                   *operation.DeclaredIntent         `json:"intent,omitempty"`
	Evidence                 *coreevidence.Contract            `json:"evidence,omitempty"`
	Freshness                environmentcore.Freshness         `json:"freshness,omitempty"`
	Execution                *environmentcore.ExecutionContext `json:"execution,omitempty"`
	ProcessTarget            *processcore.Target               `json:"process_target,omitempty"`
	IncludePorts             bool                              `json:"include_ports,omitempty"`
	CWD                      string                            `json:"cwd,omitempty"`
	TTY                      bool                              `json:"tty,omitempty"`
	Persistent               bool                              `json:"persistent,omitempty"`
	SessionName              string                            `json:"session_name,omitempty"`
	TimeoutMS                int64                             `json:"timeout_ms,omitempty"`
	StdinMode                operation.StdinMode               `json:"stdin_mode,omitempty"`
	TimeoutMode              operation.TimeoutMode             `json:"timeout_mode,omitempty"`
	TraceMode                trace.Mode                        `json:"trace_mode,omitempty"`
	ResourceLimits           *operation.ResourceLimits         `json:"limits,omitempty"`
	Hermetic                 *hermeticcore.Request             `json:"hermetic,omitempty"`
	YieldMS                  int64                             `json:"yield_time_ms,omitempty"`
	MaxOutputBytes           int                               `json:"max_output_bytes,omitempty"`
	SessionID                string                            `json:"session_id,omitempty"`
	Selector                 *outputview.Selector              `json:"selector,omitempty"`
	Cursor                   int64                             `json:"cursor,omitempty"`
	InputOffset              int64                             `json:"input_offset,omitempty"`
	Chars                    string                            `json:"chars,omitempty"`
	EOF                      bool                              `json:"eof,omitempty"`
	KillID                   string                            `json:"kill_id,omitempty"`
	Signal                   string                            `json:"signal,omitempty"`
	Target                   *observationcore.Target           `json:"target,omitempty"`
	AfterEventCursor         string                            `json:"after_event_cursor,omitempty"`
	MaxEvents                int                               `json:"max_events,omitempty"`
	RecordKind               structuredcore.RecordKind         `json:"record_kind,omitempty"`
	Severity                 structuredcore.Severity           `json:"severity,omitempty"`
	Path                     string                            `json:"path,omitempty"`
	TestStatus               structuredcore.TestStatus         `json:"test_status,omitempty"`
	State                    string                            `json:"state,omitempty"`
	PersistentOnly           *bool                             `json:"persistent_only,omitempty"`
	Continuation             string                            `json:"continuation,omitempty"`
	MaxRecords               int                               `json:"max_records,omitempty"`
	EvidenceID               string                            `json:"evidence_id,omitempty"`
	VerificationKind         coreevidence.VerificationKind     `json:"verification_kind,omitempty"`
	EvidenceResult           coreevidence.Result               `json:"result,omitempty"`
	RevalidateArtifacts      bool                              `json:"revalidate_artifacts,omitempty"`
	MaxSamples               int                               `json:"max_samples,omitempty"`
	MaxResources             int                               `json:"max_resources,omitempty"`
	ReproCreateID            string                            `json:"repro_create_id,omitempty"`
	CapturePolicy            *reprocore.CapturePolicy          `json:"capture_policy,omitempty"`
	ReproID                  string                            `json:"repro_id,omitempty"`
	MutationID               string                            `json:"mutation_id,omitempty"`
	ScopeID                  string                            `json:"scope_id,omitempty"`
	Mode                     mutationscopecore.Mode            `json:"mode,omitempty"`
	Paths                    []string                          `json:"paths,omitempty"`
	TTLMS                    int64                             `json:"ttl_ms,omitempty"`
	VerificationRequestV2Fields
}

type ResponseV2 struct {
	NegotiatedMedia                  *capability.NegotiatedMedia         `json:"negotiated_media,omitempty"`
	Media                            *media.Result                       `json:"media,omitempty"`
	IPVersion                        int                                 `json:"ipc_version"`
	Kind                             string                              `json:"kind"`
	RequestID                        string                              `json:"request_id"`
	Action                           string                              `json:"action"`
	OK                               bool                                `json:"ok"`
	View                             *app.View                           `json:"view,omitempty"`
	Result                           *receipt.Result                     `json:"result,omitempty"`
	Checkpoint                       *checkpointcore.Checkpoint          `json:"checkpoint,omitempty"`
	Restore                          *checkpointcore.RestoreResult       `json:"restore,omitempty"`
	CheckpointInspection             *checkpointapp.CheckpointInspection `json:"checkpoint_inspection,omitempty"`
	Server                           *capability.Catalog                 `json:"server,omitempty"`
	Project                          *project.Inspection                 `json:"project,omitempty"`
	Readiness                        *project.Readiness                  `json:"readiness,omitempty"`
	Workspace                        *workspace.Workspace                `json:"workspace,omitempty"`
	Activity                         *activity.Activity                  `json:"activity,omitempty"`
	Events                           *observationapp.InspectResult       `json:"events,omitempty"`
	Structured                       *structuredapp.InspectResult        `json:"structured,omitempty"`
	Evidence                         *evidenceapp.InspectResult          `json:"evidence,omitempty"`
	Environment                      *environmentcore.Snapshot           `json:"environment,omitempty"`
	Process                          *processcore.Observation            `json:"process,omitempty"`
	Mutation                         *mutationscopeapp.MutationResult    `json:"mutation,omitempty"`
	MutationScopes                   *mutationscopecore.InspectResult    `json:"mutation_scopes,omitempty"`
	ActiveMutationScopes             []mutationscopecore.Scope           `json:"active_mutation_scopes,omitempty"`
	MutationScopeAdvisories          []mutationscopecore.Advisory        `json:"mutation_scope_advisories,omitempty"`
	MutationScopesTruncated          bool                                `json:"mutation_scopes_truncated,omitempty"`
	MutationScopeAdvisoriesTruncated bool                                `json:"mutation_scope_advisories_truncated,omitempty"`
	Telemetry                        *telemetryapp.InspectResult         `json:"telemetry,omitempty"`
	InputTrace                       *inputtraceapp.InspectResult        `json:"input_trace,omitempty"`
	Capsule                          *reprocore.Capsule                  `json:"capsule,omitempty"`
	Repro                            *reproapp.InspectResult             `json:"repro,omitempty"`
	Code                             *codeintel.Result                   `json:"code,omitempty"`
	OutputView                       *outputview.Result                  `json:"output_view,omitempty"`
	Sessions                         *persistent.InspectPage             `json:"sessions,omitempty"`
	VerificationResponseV2Fields
	Error *Error `json:"error,omitempty"`
}

type v2Header struct {
	IPVersion int    `json:"ipc_version"`
	Kind      string `json:"kind"`
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
}

func decodeRequestV2(r io.Reader) (RequestV2, error) {
	data, err := readBoundedJSON(r)
	if err != nil {
		return RequestV2{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	var header v2Header
	if err := json.Unmarshal(data, &header); err != nil {
		return RequestV2{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	partial := RequestV2{IPVersion: header.IPVersion, Kind: header.Kind, RequestID: header.RequestID, Action: header.Action}
	if header.IPVersion != ipcV2 {
		return partial, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "ipc_version", "required_version": "2"}, fmt.Errorf("unsupported ipc version"))
	}
	if header.Kind != "request" {
		return partial, failure.New(failure.InvalidInput, map[string]string{"field": "kind"}, fmt.Errorf("invalid v2 kind"))
	}
	if isDeferredV2Action(header.Action) {
		return partial, failure.New(failure.FeatureUnavailable, map[string]string{"feature": header.Action}, fmt.Errorf("unsupported v2 feature"))
	}
	if !isSupportedV2Action(header.Action) {
		return partial, failure.New(failure.InvalidInput, map[string]string{"field": "action"}, fmt.Errorf("unknown v2 action"))
	}
	if err := validateV2FieldSet(data, header.Action); err != nil {
		return partial, err
	}
	var out RequestV2
	if err := strictDecodeV2(data, &out); err != nil {
		return partial, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_request"}, err)
	}
	if err := validateRequestV2(out); err != nil {
		return out, err
	}
	return out, nil
}

func validateV2FieldSet(data []byte, action string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	allowed := map[string]bool{"ipc_version": true, "kind": true, "request_id": true, "action": true}
	for _, field := range actionFieldsV2(action) {
		allowed[field] = true
	}
	for field := range fields {
		if !allowed[field] {
			return failure.New(failure.InvalidInput, map[string]string{"field": field}, fmt.Errorf("unexpected field"))
		}
	}
	return nil
}

func validateRequestV2(v RequestV2) error {
	if v.IPVersion != ipcV2 {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "ipc_version", "required_version": "2"}, fmt.Errorf("unsupported ipc version"))
	}
	if v.Kind != "request" {
		return failure.New(failure.InvalidInput, map[string]string{"field": "kind"}, fmt.Errorf("invalid v2 kind"))
	}
	if isDeferredV2Action(v.Action) {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": v.Action}, fmt.Errorf("unsupported v2 feature"))
	}
	if !isSupportedV2Action(v.Action) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, fmt.Errorf("unknown v2 action"))
	}
	if bridgeVerificationActionV2(v.Action) {
		return validateVerificationRequestV2(v)
	}
	if v.Action == "capabilities.negotiate" || v.Action == "read_media" {
		return validateMediaRequestV2(v)
	}
	switch v.Action {
	case "start":
		return validateStartRequestV2(v)
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

func validateStartRequestV2(v RequestV2) error {
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
		intent := operation.TypedRequestIntent{WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, Params: v.Params, TTY: v.TTY, TimeoutMS: v.TimeoutMS, Hermetic: v.Hermetic}
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
	if _, err := trace.NormalizeMode(v.TraceMode); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	return nil
}

func validateA4RequestV2(v RequestV2) error {
	switch v.Action {
	case "inspect.telemetry":
		if _, err := operation.ParseID(v.OperationID); err != nil || v.MaxSamples < 1 || v.MaxSamples > telemetryapp.MaxInspectSamples {
			return failure.New(failure.InvalidInput, map[string]string{"field": "inspect.telemetry"}, fmt.Errorf("invalid telemetry inspection"))
		}
	case "repro.create":
		policy := reprocore.CapturePolicy{DependentDerivations: reprocore.CaptureCurrent}
		if v.CapturePolicy != nil {
			policy = *v.CapturePolicy
		}
		if err := (reprocore.CreateRequest{CreateID: v.ReproCreateID, OperationID: v.OperationID, Policy: policy}).Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "repro.create"}, err)
		}
	case "inspect.repro":
		if !validReproIDV2(v.ReproID) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "repro_id"}, fmt.Errorf("invalid repro id"))
		}
	}
	return nil
}

func validateCodeInspectV2(v RequestV2) error {
	if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
	}
	if v.ActivityID != "" {
		if _, err := activity.ParseID(v.ActivityID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
		}
	}
	if v.CodeQuery == nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "code_query"}, fmt.Errorf("code query missing"))
	}
	if err := v.CodeQuery.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "code_query"}, err)
	}
	return nil
}

func validateStructuredInspectV2(v RequestV2) error {
	request := structuredapp.InspectRequest{OperationID: v.OperationID, Filter: structuredapp.RecordFilter{RecordKind: v.RecordKind, Severity: v.Severity, Path: v.Path, TestStatus: v.TestStatus}, Continuation: v.Continuation, MaxRecords: v.MaxRecords}
	if err := request.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "inspect.structured"}, err)
	}
	return nil
}

func validateEvidenceInspectV2(v RequestV2) error {
	req := evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{EvidenceID: v.EvidenceID, OperationID: v.OperationID, WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, ActivityID: v.ActivityID, VerificationKind: v.VerificationKind, Result: v.EvidenceResult, RevalidateArtifacts: v.RevalidateArtifacts}, Continuation: v.Continuation, MaxRecords: v.MaxRecords}
	if _, err := evidenceapp.NormalizeInspectRequestForTransport(req); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "inspect.evidence"}, err)
	}
	return nil
}

func validateEnvironmentInspectV2(v RequestV2) error {
	if v.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
	}
	if v.Freshness != "" && v.Freshness != environmentcore.FreshnessCached && v.Freshness != environmentcore.FreshnessRefresh {
		return failure.New(failure.InvalidInput, map[string]string{"field": "freshness"}, fmt.Errorf("invalid environment freshness"))
	}
	if v.Execution != nil && ((v.Execution.Mode != "shell" && v.Execution.Mode != "argv") || v.Execution.Identity == "") {
		return failure.New(failure.InvalidInput, map[string]string{"field": "execution"}, fmt.Errorf("invalid execution identity"))
	}
	return nil
}

func validateProcessInspectV2(v RequestV2) error {
	if v.ProcessTarget == nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "process_target"}, fmt.Errorf("process target missing"))
	}
	if err := v.ProcessTarget.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "process_target"}, err)
	}
	return nil
}

func validateEventInspectV2(v RequestV2) error {
	if v.Target == nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "target"}, fmt.Errorf("event target missing"))
	}
	if err := v.Target.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "target"}, err)
	}
	if v.MaxEvents < 1 || v.MaxEvents > observationapp.MaxInspectEvents {
		return failure.New(failure.InvalidInput, map[string]string{"field": "max_events"}, fmt.Errorf("invalid max events"))
	}
	if v.AfterEventCursor != "" && (!strings.HasPrefix(v.AfterEventCursor, observationapp.EventCursorPrefix) || len(v.AfterEventCursor) > observationapp.MaxEventCursorBytes) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "after_event_cursor"}, fmt.Errorf("invalid event cursor"))
	}
	return nil
}

func validReproIDV2(value string) bool {
	if len(value) != 32 || !strings.HasPrefix(value, "repro_") {
		return false
	}
	for _, r := range value[6:] {
		if strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			continue
		}
		return false
	}
	return true
}
