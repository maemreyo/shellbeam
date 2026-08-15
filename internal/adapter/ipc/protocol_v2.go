package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const ipcV2 = 2

type RequestV2 struct {
	IPVersion         int                       `json:"ipc_version"`
	Kind              string                    `json:"kind"`
	RequestID         string                    `json:"request_id"`
	Action            string                    `json:"action"`
	OperationID       string                    `json:"operation_id,omitempty"`
	WorkspaceID       string                    `json:"workspace_id,omitempty"`
	ActivityID        string                    `json:"activity_id,omitempty"`
	CodeQuery         *codeintel.Query          `json:"code_query,omitempty"`
	WorkspaceHint     *workspace.Hint           `json:"workspace_hint,omitempty"`
	StructuredAdapter string                    `json:"structured_adapter,omitempty"`
	ProjectCommandID  string                    `json:"project_command_id,omitempty"`
	Params            map[string]string         `json:"params,omitempty"`
	Command           string                    `json:"command,omitempty"`
	Argv              []string                  `json:"argv,omitempty"`
	Intent            *operation.DeclaredIntent `json:"intent,omitempty"`
	CWD               string                    `json:"cwd,omitempty"`
	TTY               bool                      `json:"tty,omitempty"`
	TimeoutMS         int64                     `json:"timeout_ms,omitempty"`
	YieldMS           int64                     `json:"yield_time_ms,omitempty"`
	MaxOutputBytes    int                       `json:"max_output_bytes,omitempty"`
	SessionID         string                    `json:"session_id,omitempty"`
	Selector          *outputview.Selector      `json:"selector,omitempty"`
	Cursor            int64                     `json:"cursor,omitempty"`
	InputOffset       int64                     `json:"input_offset,omitempty"`
	Chars             string                    `json:"chars,omitempty"`
	EOF               bool                      `json:"eof,omitempty"`
	KillID            string                    `json:"kill_id,omitempty"`
	Signal            string                    `json:"signal,omitempty"`
	Target            *observationcore.Target   `json:"target,omitempty"`
	AfterEventCursor  string                    `json:"after_event_cursor,omitempty"`
	MaxEvents         int                       `json:"max_events,omitempty"`
	RecordKind        structuredcore.RecordKind `json:"record_kind,omitempty"`
	Severity          structuredcore.Severity   `json:"severity,omitempty"`
	Path              string                    `json:"path,omitempty"`
	TestStatus        structuredcore.TestStatus `json:"test_status,omitempty"`
	Continuation      string                    `json:"continuation,omitempty"`
	MaxRecords        int                       `json:"max_records,omitempty"`
	MaxSamples        int                       `json:"max_samples,omitempty"`
	ReproCreateID     string                    `json:"repro_create_id,omitempty"`
	CapturePolicy     *reprocore.CapturePolicy  `json:"capture_policy,omitempty"`
	ReproID           string                    `json:"repro_id,omitempty"`
}

type ResponseV2 struct {
	IPVersion  int                           `json:"ipc_version"`
	Kind       string                        `json:"kind"`
	RequestID  string                        `json:"request_id"`
	Action     string                        `json:"action"`
	OK         bool                          `json:"ok"`
	View       *app.View                     `json:"view,omitempty"`
	Result     *receipt.Result               `json:"result,omitempty"`
	Server     *capability.Catalog           `json:"server,omitempty"`
	Project    *project.Inspection           `json:"project,omitempty"`
	Readiness  *project.Readiness            `json:"readiness,omitempty"`
	Workspace  *workspace.Workspace          `json:"workspace,omitempty"`
	Activity   *activity.Activity            `json:"activity,omitempty"`
	Events     *observationapp.InspectResult `json:"events,omitempty"`
	Structured *structuredapp.InspectResult  `json:"structured,omitempty"`
	Telemetry  *telemetryapp.InspectResult   `json:"telemetry,omitempty"`
	Capsule    *reprocore.Capsule            `json:"capsule,omitempty"`
	Repro      *reproapp.InspectResult       `json:"repro,omitempty"`
	Code       *codeintel.Result             `json:"code,omitempty"`
	OutputView *outputview.Result            `json:"output_view,omitempty"`
	Error      *Error                        `json:"error,omitempty"`
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

func actionFieldsV2(action string) []string {
	switch action {
	case "start":
		return []string{"operation_id", "workspace_id", "activity_id", "workspace_hint", "structured_adapter", "project_command_id", "params", "command", "argv", "intent", "cwd", "tty", "timeout_ms", "yield_time_ms", "max_output_bytes"}
	case "poll":
		return []string{"session_id", "cursor", "yield_time_ms", "max_output_bytes"}
	case "read_output":
		return []string{"session_id", "selector", "continuation"}
	case "write":
		return []string{"session_id", "input_offset", "chars", "eof"}
	case "kill":
		return []string{"session_id", "kill_id", "signal"}
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		return []string{"workspace_id"}
	case "inspect.activity":
		return []string{"activity_id"}
	case "inspect.events":
		return []string{"target", "after_event_cursor", "max_events"}
	case "inspect.structured":
		return []string{"operation_id", "record_kind", "severity", "path", "test_status", "continuation", "max_records"}
	case "inspect.telemetry":
		return []string{"operation_id", "max_samples"}
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
	case "inspect.structured":
		return validateStructuredInspectV2(v)
	case "inspect.telemetry", "repro.create", "inspect.repro":
		return validateA4RequestV2(v)
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
		intent := operation.TypedRequestIntent{WorkspaceID: v.WorkspaceID, ProjectCommandID: v.ProjectCommandID, Params: v.Params, TTY: v.TTY, TimeoutMS: v.TimeoutMS}
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

func isSupportedV2Action(action string) bool {
	switch action {
	case "start", "poll", "write", "kill", "read_output", "inspect.server", "inspect.workspace", "inspect.activity", "inspect.project", "inspect.readiness", "inspect.events", "inspect.structured", "inspect.telemetry", "repro.create", "inspect.repro", "inspect.code":
		return true
	default:
		return false
	}
}

func isDeferredV2Action(string) bool { return false }

func readBoundedJSON(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, (1<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("request too large")
	}
	return data, nil
}

func strictDecodeV2(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing json")
	}
	return nil
}
