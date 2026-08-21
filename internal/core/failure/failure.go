// Package failure defines stable public failure facts and safe boundary projection.
package failure

import (
	"errors"
	"strings"
	"unicode"
)

type Code string

func (c Code) Error() string { return string(c) }

const (
	InvalidInput                      Code = "invalid_input"
	FeatureUnavailable                Code = "feature_unavailable"
	OperationConflict                 Code = "operation_conflict"
	OperationMetadataConflict         Code = "operation_metadata_conflict"
	WorkspaceNotFound                 Code = "workspace_not_found"
	DecisionContextUnavailable        Code = "decision_context_unavailable"
	WorkspaceStale                    Code = "workspace_stale"
	WorkspaceRootMissing              Code = "workspace_root_missing"
	ActivityNotFound                  Code = "activity_not_found"
	WorkspaceAddressEscape            Code = "workspace_address_escape"
	ManifestInvalid                   Code = "manifest_invalid"
	ManifestReviewRequired            Code = "manifest_review_required"
	ProjectReadinessUnavailable       Code = "project_readiness_unavailable"
	ProjectRequirementInvalid         Code = "project_requirement_invalid"
	ToolchainMissing                  Code = "toolchain_missing"
	ToolchainVersionUnknown           Code = "toolchain_version_unknown"
	ToolchainIncompatible             Code = "toolchain_incompatible"
	ProjectCommandNotFound            Code = "project_command_not_found"
	ProjectCommandNotParameterized    Code = "project_command_not_parameterized"
	ParameterUnknown                  Code = "parameter_unknown"
	ParameterMissing                  Code = "parameter_missing"
	ParameterInvalid                  Code = "parameter_invalid"
	ParameterKindUnsupported          Code = "parameter_kind_unsupported"
	ParameterValidationUnavailable    Code = "parameter_validation_unavailable"
	ProjectCommandBindingConflict     Code = "project_command_binding_conflict"
	OutputCompacted                   Code = "output_compacted"
	OutputUnavailable                 Code = "output_unavailable"
	OutputOutOfRange                  Code = "output_out_of_range"
	OutputContinuationInvalid         Code = "output_continuation_invalid"
	OutputContinuationExpired         Code = "output_continuation_expired"
	IdentityObservationFailed         Code = "identity_observation_failed"
	EventCursorInvalid                Code = "event_cursor_invalid"
	EventCursorExpired                Code = "event_cursor_expired"
	EvidenceCursorInvalid             Code = "evidence_cursor_invalid"
	EvidenceCursorExpired             Code = "evidence_cursor_expired"
	EventContinuityUnavailable        Code = "event_continuity_unavailable"
	StructuredAdapterUnavailable      Code = "structured_adapter_unavailable"
	StructuredAdapterUnsupported      Code = "structured_adapter_unsupported"
	StructuredResultMalformed         Code = "structured_result_malformed"
	StructuredResultPartial           Code = "structured_result_partial"
	StructuredResultBudgetExceeded    Code = "structured_result_budget_exceeded"
	StructuredResultNotFound          Code = "structured_result_not_found"
	TelemetryUnavailable              Code = "telemetry_unavailable"
	TelemetryPartial                  Code = "telemetry_partial"
	TelemetryBudgetExceeded           Code = "telemetry_budget_exceeded"
	TelemetryIncompatibleHistory      Code = "telemetry_incompatible_history"
	ResourceObservationUnavailable    Code = "resource_observation_unavailable"
	ResourceObservationPartial        Code = "resource_observation_partial"
	ResourceLimitUnsupported          Code = "resource_limit_unsupported"
	ReproNotFound                     Code = "repro_not_found"
	ReproMaterializationUnavailable   Code = "repro_materialization_unavailable"
	ReproSourceUnavailable            Code = "repro_source_unavailable"
	ReproReferenceCompacted           Code = "repro_reference_compacted"
	EnvironmentObservationUnavailable Code = "environment_observation_unavailable"
	ToolchainProbeUnavailable         Code = "toolchain_probe_unavailable"
	ToolchainProbeTimeout             Code = "toolchain_probe_timeout"
	ToolchainProbeUnsupported         Code = "toolchain_probe_unsupported"
	ProcessNotFound                   Code = "process_not_found"
	ProcessAccessDenied               Code = "process_access_denied"
	ProcessIdentityChanged            Code = "process_identity_changed"
	ProcessObservationIncomplete      Code = "process_observation_incomplete"
	ProcessLimitExceeded              Code = "process_limit_exceeded"
	PortObservationUnavailable        Code = "port_observation_unavailable"
	MutationScopeInvalid              Code = "mutation_scope_invalid"
	MutationScopeBindingConflict      Code = "mutation_scope_binding_conflict"
	MutationMetadataConflict          Code = "mutation_metadata_conflict"
	MutationScopeCapacityExceeded     Code = "mutation_scope_capacity_exceeded"
	PersistentSessionNameConflict     Code = "persistent_session_name_conflict"
	PersistentSessionOwnershipLost    Code = "persistent_session_ownership_lost"
	SupervisorUnavailable             Code = "supervisor_unavailable"
	SupervisorProtocolMismatch        Code = "supervisor_protocol_mismatch"
	SupervisorAuthFailed              Code = "supervisor_auth_failed"
	SupervisorStateConflict           Code = "supervisor_state_conflict"
	PersistentRecoveryOutputConflict  Code = "persistent_recovery_output_conflict"
	PersistentHistoryExhausted        Code = "persistent_history_exhausted"
	PersistentInputHistoryExhausted   Code = "persistent_input_history_exhausted"
	PersistentKillHistoryExhausted    Code = "persistent_kill_history_exhausted"
	PersistenceAmbiguous              Code = "persistence_ambiguous"
	InvalidDaemonResponse             Code = "invalid_daemon_response"
	RuntimeVersionMismatch            Code = "runtime_version_mismatch"
	MediaPathNotFound                 Code = "media_path_not_found"
	MediaPathUnsafe                   Code = "media_path_unsafe"
	MediaNotRegular                   Code = "media_not_regular"
	MediaTooLarge                     Code = "media_too_large"
	MediaTypeUnsupported              Code = "media_type_unsupported"
	MediaInvalidImage                 Code = "media_invalid_image"
	MediaDimensionsExceeded           Code = "media_dimensions_exceeded"
	MediaSourceChanged                Code = "media_source_changed"
	MediaReadTimeout                  Code = "media_read_timeout"
	MediaReadFailed                   Code = "media_read_failed"
	CapacityExceeded                  Code = "capacity_exceeded"
	SessionNotFound                   Code = "session_not_found"
	PersistenceUnavailable            Code = "persistence_unavailable"
	StorageReserveExhausted           Code = "storage_reserve_exhausted"
	Internal                          Code = "internal"
)

const (
	CheckpointProviderUnavailable          Code = "checkpoint_provider_unavailable"
	CheckpointCreateConflict               Code = "checkpoint_create_conflict"
	CheckpointScopeInvalid                 Code = "checkpoint_scope_invalid"
	CheckpointScopeTooLarge                Code = "checkpoint_scope_too_large"
	CheckpointPathUnsupported              Code = "checkpoint_path_unsupported"
	CheckpointSubmoduleBoundaryUnsupported Code = "checkpoint_submodule_boundary_unsupported"
	CheckpointBudgetExceeded               Code = "checkpoint_budget_exceeded"
	CheckpointNotFound                     Code = "checkpoint_not_found"
	CheckpointExpired                      Code = "checkpoint_expired"
	CheckpointRestoreRequestConflict       Code = "checkpoint_restore_request_conflict"
	CheckpointRestoreConflict              Code = "checkpoint_restore_conflict"
	CheckpointRestorePartial               Code = "checkpoint_restore_partial"
	CheckpointRestoreFailed                Code = "checkpoint_restore_failed"

	InputTraceProviderUnavailable   Code = "input_trace_provider_unavailable"
	InputTraceRequiredUnavailable   Code = "input_trace_required_unavailable"
	InputTraceStartupBudgetExceeded Code = "input_trace_startup_budget_exceeded"
	InputTraceUnsupported           Code = "input_trace_unsupported"
	InputTracePartial               Code = "input_trace_partial"
	InputTraceBudgetExceeded        Code = "input_trace_budget_exceeded"
	InputTraceLateAttach            Code = "input_trace_late_attach"
	InputTraceOwnershipLost         Code = "input_trace_ownership_lost"
	InputTraceNotFound              Code = "input_trace_not_found"
)

type Failure struct {
	Code      Code              `json:"code"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Details   map[string]string `json:"details,omitempty"`
	Cause     error             `json:"-"`
}

type publicSpec struct {
	message   string
	retryable bool
	details   map[string]bool
}

var publicSpecs = map[Code]publicSpec{
	InvalidInput:                      {message: "invalid input", details: keys("field", "reason")},
	FeatureUnavailable:                {message: "feature unavailable", details: keys("feature", "required_version")},
	OperationConflict:                 {message: "operation conflicts with an existing intent", details: keys("operation_id")},
	OperationMetadataConflict:         {message: "operation metadata conflicts with existing metadata", details: keys("operation_id", "field")},
	WorkspaceNotFound:                 {message: "workspace not found", details: keys("workspace_id")},
	DecisionContextUnavailable:        {message: "decision context unavailable"},
	WorkspaceStale:                    {message: "workspace registration is stale", details: keys("workspace_id", "reason")},
	WorkspaceRootMissing:              {message: "workspace root is missing", details: keys("workspace_id", "reason")},
	ActivityNotFound:                  {message: "activity not found", details: keys("activity_id")},
	WorkspaceAddressEscape:            {message: "workspace address escapes registered root", details: keys("workspace_id", "cwd")},
	ManifestInvalid:                   {message: "project manifest is invalid", details: keys("field", "reason")},
	ManifestReviewRequired:            {message: "project manifest review required", details: keys("manifest_version", "reason")},
	ProjectReadinessUnavailable:       {message: "project readiness unavailable", retryable: true, details: keys("workspace_id", "reason")},
	ProjectRequirementInvalid:         {message: "project requirement invalid", details: keys("requirement", "kind", "reason")},
	ToolchainMissing:                  {message: "required toolchain missing", details: keys("toolchain")},
	ToolchainVersionUnknown:           {message: "toolchain version unknown", retryable: true, details: keys("toolchain", "reason")},
	ToolchainIncompatible:             {message: "toolchain incompatible", details: keys("toolchain", "reason")},
	ProjectCommandNotFound:            {message: "project command not found", details: keys("command")},
	ProjectCommandNotParameterized:    {message: "project command is not parameterized", details: keys("command")},
	ParameterUnknown:                  {message: "project command parameter unknown", details: keys("command", "parameter")},
	ParameterMissing:                  {message: "project command parameter missing", details: keys("command", "parameter")},
	ParameterInvalid:                  {message: "project command parameter invalid", details: keys("command", "parameter", "kind", "reason")},
	ParameterKindUnsupported:          {message: "project command parameter kind unsupported", details: keys("command", "parameter", "kind")},
	ParameterValidationUnavailable:    {message: "project command parameter validation unavailable", retryable: true, details: keys("command", "parameter", "kind", "provider", "reason")},
	ProjectCommandBindingConflict:     {message: "project command binding conflicts with admitted operation", details: keys("operation_id", "command")},
	OutputCompacted:                   {message: "retained output has been compacted", details: keys("session_id")},
	OutputUnavailable:                 {message: "retained output unavailable", retryable: true, details: keys("session_id", "reason")},
	OutputOutOfRange:                  {message: "output selector is out of range", details: keys("session_id", "reason")},
	OutputContinuationInvalid:         {message: "output continuation is invalid", details: keys("reason")},
	OutputContinuationExpired:         {message: "output continuation has expired", details: keys("reason")},
	IdentityObservationFailed:         {message: "identity observation failed", retryable: true, details: keys("provider", "reason")},
	EventCursorInvalid:                {message: "event cursor is invalid", details: keys("reason")},
	EventCursorExpired:                {message: "event cursor has expired", details: keys("reason")},
	EvidenceCursorInvalid:             {message: "evidence cursor is invalid", details: keys("reason")},
	EvidenceCursorExpired:             {message: "evidence cursor has expired", details: keys("reason")},
	EventContinuityUnavailable:        {message: "event continuity unavailable", retryable: true, details: keys("reason")},
	StructuredAdapterUnavailable:      {message: "structured adapter unavailable", retryable: true, details: keys("adapter", "reason")},
	StructuredAdapterUnsupported:      {message: "structured adapter unsupported", details: keys("adapter")},
	StructuredResultMalformed:         {message: "structured result malformed", details: keys("adapter", "reason")},
	StructuredResultPartial:           {message: "structured result partial", details: keys("adapter", "reason")},
	StructuredResultBudgetExceeded:    {message: "structured result budget exceeded", retryable: true, details: keys("adapter", "reason")},
	StructuredResultNotFound:          {message: "structured result not found", details: keys("operation_id", "derivation_key")},
	TelemetryUnavailable:              {message: "execution telemetry unavailable", retryable: true, details: keys("operation_id", "reason")},
	TelemetryPartial:                  {message: "execution telemetry partial", details: keys("operation_id", "reason")},
	TelemetryBudgetExceeded:           {message: "execution telemetry budget exceeded", retryable: true, details: keys("operation_id", "reason")},
	TelemetryIncompatibleHistory:      {message: "execution telemetry history incompatible", details: keys("operation_id", "reason")},
	ResourceObservationUnavailable:    {message: "resource observation unavailable", details: keys("metric", "reason")},
	ResourceObservationPartial:        {message: "resource observation partial", details: keys("metric", "reason")},
	ResourceLimitUnsupported:          {message: "resource limit unsupported", details: keys("metric", "reason")},
	ReproNotFound:                     {message: "reproduction capsule not found", details: keys("repro_id")},
	ReproMaterializationUnavailable:   {message: "reproduction materialization unavailable", retryable: true, details: keys("operation_id", "reason")},
	ReproSourceUnavailable:            {message: "reproduction source unavailable", details: keys("repro_id", "reason")},
	ReproReferenceCompacted:           {message: "reproduction reference compacted", details: keys("repro_id", "ref_id")},
	EnvironmentObservationUnavailable: {message: "environment observation unavailable", retryable: true, details: keys("reason")},
	ToolchainProbeUnavailable:         {message: "toolchain probe unavailable", retryable: true, details: keys("toolchain", "reason")},
	ToolchainProbeTimeout:             {message: "toolchain probe timed out", retryable: true, details: keys("toolchain")},
	ToolchainProbeUnsupported:         {message: "toolchain probe unsupported", details: keys("toolchain")},
	ProcessNotFound:                   {message: "process not found", details: keys("pid")},
	ProcessAccessDenied:               {message: "process access denied", details: keys("pid")},
	ProcessIdentityChanged:            {message: "process identity changed during observation", retryable: true, details: keys("pid")},
	ProcessObservationIncomplete:      {message: "process observation incomplete", retryable: true, details: keys("pid", "reason")},
	ProcessLimitExceeded:              {message: "process observation limit exceeded", details: keys("reason")},
	PortObservationUnavailable:        {message: "port observation unavailable", retryable: true, details: keys("pid", "reason")},
	MutationScopeInvalid:              {message: "mutation scope is invalid", details: keys("scope_id", "field", "reason")},
	MutationScopeBindingConflict:      {message: "mutation scope binding conflicts with existing binding", details: keys("scope_id")},
	MutationMetadataConflict:          {message: "mutation metadata conflicts with existing mutation", details: keys("mutation_id", "scope_id", "field")},
	MutationScopeCapacityExceeded:     {message: "mutation scope capacity exceeded", retryable: true, details: keys("scope_id", "workspace_id", "activity_id", "reason")},
	PersistentSessionNameConflict:     {message: "persistent session name conflicts with existing binding", details: keys("session_id", "session_name", "reason")},
	PersistentSessionOwnershipLost:    {message: "persistent session ownership lost", details: keys("session_id", "session_name", "reason")},
	SupervisorUnavailable:             {message: "persistent session supervisor unavailable", retryable: true, details: keys("session_id", "session_name", "reason")},
	SupervisorProtocolMismatch:        {message: "persistent session supervisor protocol mismatch", details: keys("session_id", "session_name", "reason", "required_version")},
	SupervisorAuthFailed:              {message: "persistent session supervisor authentication failed", details: keys("session_id", "session_name", "reason")},
	SupervisorStateConflict:           {message: "persistent session supervisor state conflict", details: keys("session_id", "session_name", "reason")},
	PersistentRecoveryOutputConflict:  {message: "persistent recovery output conflicts with canonical output", details: keys("session_id", "reason")},
	PersistentHistoryExhausted:        {message: "persistent session history capacity exhausted", retryable: true, details: keys("session_id", "reason")},
	PersistentInputHistoryExhausted:   {message: "persistent input history capacity exhausted", retryable: true, details: keys("session_id", "reason")},
	PersistentKillHistoryExhausted:    {message: "persistent kill history capacity exhausted", retryable: true, details: keys("session_id", "reason")},
	PersistenceAmbiguous:              {message: "persistence result is ambiguous", retryable: true},
	InvalidDaemonResponse:             {message: "invalid daemon response"},
	RuntimeVersionMismatch:            {message: "ShellBeam MCP and daemon builds do not match", details: keys("mcp_revision", "daemon_revision", "reason", "recovery")},
	MediaPathNotFound:                 {message: "media path not found"},
	MediaPathUnsafe:                   {message: "media path is unsafe"},
	MediaNotRegular:                   {message: "media path is not a regular file"},
	MediaTooLarge:                     {message: "media exceeds byte limit", details: keys("limit", "byte_size")},
	MediaTypeUnsupported:              {message: "media type unsupported", details: keys("mime_type", "format")},
	MediaInvalidImage:                 {message: "media image is invalid", details: keys("format", "reason")},
	MediaDimensionsExceeded:           {message: "media dimensions exceed limits", details: keys("width", "height", "limit")},
	MediaSourceChanged:                {message: "media source changed during read", retryable: true},
	MediaReadTimeout:                  {message: "media read timed out"},
	MediaReadFailed:                   {message: "media read failed", details: keys("reason")},
	CapacityExceeded:                  {message: "capacity exceeded", retryable: true, details: keys("active", "limit", "retry_after_ms")},
	SessionNotFound:                   {message: "session not found", details: keys("session_id", "reason")},
	PersistenceUnavailable:            {message: "persistence unavailable", retryable: true},
	StorageReserveExhausted:           {message: "storage reserve exhausted", retryable: true},
	Internal:                          {message: "internal error"},

	CheckpointProviderUnavailable:          {message: "checkpoint provider unavailable", retryable: true, details: keys("provider", "reason")},
	CheckpointCreateConflict:               {message: "checkpoint create request conflicts with existing binding", details: keys("checkpoint_create_id")},
	CheckpointScopeInvalid:                 {message: "checkpoint scope is invalid", details: keys("field", "reason")},
	CheckpointScopeTooLarge:                {message: "checkpoint scope exceeds limit", details: keys("field", "reason", "limit")},
	CheckpointPathUnsupported:              {message: "checkpoint path is unsupported", details: keys("path", "reason")},
	CheckpointSubmoduleBoundaryUnsupported: {message: "checkpoint submodule boundary is unsupported", details: keys("path")},
	CheckpointBudgetExceeded:               {message: "checkpoint budget exceeded", details: keys("field", "reason", "limit")},
	CheckpointNotFound:                     {message: "checkpoint not found", details: keys("checkpoint_id")},
	CheckpointExpired:                      {message: "checkpoint expired", details: keys("checkpoint_id")},
	CheckpointRestoreRequestConflict:       {message: "checkpoint restore request conflicts with existing binding", details: keys("restore_id")},
	CheckpointRestoreConflict:              {message: "checkpoint restore path conflicts with current state", details: keys("restore_id", "checkpoint_id", "path")},
	CheckpointRestorePartial:               {message: "checkpoint restore is partial", details: keys("restore_id", "checkpoint_id")},
	CheckpointRestoreFailed:                {message: "checkpoint restore failed", retryable: true, details: keys("restore_id", "checkpoint_id", "reason")},

	InputTraceProviderUnavailable:   {message: "input trace provider unavailable", retryable: true, details: keys("provider", "platform", "reason")},
	InputTraceRequiredUnavailable:   {message: "required input tracing unavailable", details: keys("provider", "platform", "reason")},
	InputTraceStartupBudgetExceeded: {message: "input trace startup budget exceeded", retryable: true, details: keys("provider", "limit_ms", "reason")},
	InputTraceUnsupported:           {message: "input tracing unsupported", details: keys("provider", "platform", "reason")},
	InputTracePartial:               {message: "input trace partial", details: keys("trace_id", "reason")},
	InputTraceBudgetExceeded:        {message: "input trace budget exceeded", details: keys("trace_id", "limit", "reason")},
	InputTraceLateAttach:            {message: "input trace attached late", details: keys("trace_id", "reason")},
	InputTraceOwnershipLost:         {message: "input trace ownership lost", details: keys("trace_id", "reason")},
	InputTraceNotFound:              {message: "input trace not found", details: keys("operation_id", "trace_id")},
}

var legacyCodes = map[string]Code{
	"operation_conflict":           OperationConflict,
	"operation_metadata_conflict":  OperationMetadataConflict,
	"capacity_exceeded":            CapacityExceeded,
	"session_not_found":            SessionNotFound,
	"persistence_unavailable":      PersistenceUnavailable,
	"storage_reserve_exhausted":    StorageReserveExhausted,
	"invalid operation id":         InvalidInput,
	"invalid session id":           InvalidInput,
	"command is empty":             InvalidInput,
	"cwd must be absolute":         InvalidInput,
	"timeout must be non-negative": InvalidInput,
	"cursor_out_of_range":          InvalidInput,
	"invalid max":                  InvalidInput,
	"session_not_live":             InvalidInput,
	"session_not_writable":         InvalidInput,
	"input_closed":                 InvalidInput,
	"empty input":                  InvalidInput,
	"input_conflict":               OperationMetadataConflict,
	"input_gap":                    InvalidInput,
	"input_backpressure":           CapacityExceeded,
	"input_eof_unsupported":        InvalidInput,
	"invalid kill_id":              InvalidInput,
	"invalid signal":               InvalidInput,
	"kill_conflict":                OperationMetadataConflict,
	"unknown_action":               InvalidInput,
}

func New(code Code, details map[string]string, cause error) *Failure {
	spec, ok := publicSpecs[code]
	if !ok {
		code = Internal
		spec = publicSpecs[Internal]
	}
	return &Failure{
		Code:      code,
		Message:   spec.message,
		Retryable: spec.retryable,
		Details:   filterDetails(spec.details, details),
		Cause:     cause,
	}
}

func Normalize(err error) error {
	if err == nil {
		return nil
	}
	var typed *Failure
	if errors.As(err, &typed) {
		return err
	}
	raw := err.Error()
	code, ok := legacyCodes[raw]
	if !ok {
		candidate := Code(raw)
		if _, known := publicSpecs[candidate]; known {
			code, ok = candidate, true
		}
	}
	if !ok {
		code = Internal
	}
	return New(code, nil, err)
}

func Public(err error) Failure {
	if err == nil {
		return *New(Internal, nil, nil)
	}
	var typed *Failure
	if errors.As(err, &typed) {
		return *New(typed.Code, typed.Details, nil)
	}
	normalized := Normalize(err)
	if errors.As(normalized, &typed) {
		return *New(typed.Code, typed.Details, nil)
	}
	return *New(Internal, nil, nil)
}

func (f *Failure) Error() string {
	if f == nil {
		return string(Internal)
	}
	return string(f.Code)
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

func (f *Failure) Is(target error) bool {
	if f == nil || target == nil {
		return false
	}
	if code, ok := target.(Code); ok {
		return f.Code == code
	}
	var other *Failure
	return errors.As(target, &other) && other.Code == f.Code
}

func keys(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func filterDetails(allowed map[string]bool, details map[string]string) map[string]string {
	if len(allowed) == 0 || len(details) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range details {
		if !allowed[key] || len(out) >= 8 {
			continue
		}
		if value = safeDetailValue(value); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func safeDetailValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := make([]rune, 0, len(value))
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
		if len(runes) == 256 {
			break
		}
		runes = append(runes, r)
	}
	return string(runes)
}
