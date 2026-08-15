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
	InvalidInput                    Code = "invalid_input"
	FeatureUnavailable              Code = "feature_unavailable"
	OperationConflict               Code = "operation_conflict"
	OperationMetadataConflict       Code = "operation_metadata_conflict"
	WorkspaceNotFound               Code = "workspace_not_found"
	ActivityNotFound                Code = "activity_not_found"
	WorkspaceAddressEscape          Code = "workspace_address_escape"
	ManifestInvalid                 Code = "manifest_invalid"
	ManifestReviewRequired          Code = "manifest_review_required"
	ProjectReadinessUnavailable     Code = "project_readiness_unavailable"
	ProjectRequirementInvalid       Code = "project_requirement_invalid"
	ToolchainMissing                Code = "toolchain_missing"
	ToolchainVersionUnknown         Code = "toolchain_version_unknown"
	ToolchainIncompatible           Code = "toolchain_incompatible"
	ProjectCommandNotFound          Code = "project_command_not_found"
	ProjectCommandNotParameterized  Code = "project_command_not_parameterized"
	ParameterUnknown                Code = "parameter_unknown"
	ParameterMissing                Code = "parameter_missing"
	ParameterInvalid                Code = "parameter_invalid"
	ParameterKindUnsupported        Code = "parameter_kind_unsupported"
	ParameterValidationUnavailable  Code = "parameter_validation_unavailable"
	ProjectCommandBindingConflict   Code = "project_command_binding_conflict"
	OutputCompacted                 Code = "output_compacted"
	OutputUnavailable               Code = "output_unavailable"
	OutputOutOfRange                Code = "output_out_of_range"
	OutputContinuationInvalid       Code = "output_continuation_invalid"
	OutputContinuationExpired       Code = "output_continuation_expired"
	IdentityObservationFailed       Code = "identity_observation_failed"
	EventCursorInvalid              Code = "event_cursor_invalid"
	EventCursorExpired              Code = "event_cursor_expired"
	EvidenceCursorInvalid           Code = "evidence_cursor_invalid"
	EvidenceCursorExpired           Code = "evidence_cursor_expired"
	EventContinuityUnavailable      Code = "event_continuity_unavailable"
	StructuredAdapterUnavailable    Code = "structured_adapter_unavailable"
	StructuredAdapterUnsupported    Code = "structured_adapter_unsupported"
	StructuredResultMalformed       Code = "structured_result_malformed"
	StructuredResultPartial         Code = "structured_result_partial"
	StructuredResultBudgetExceeded  Code = "structured_result_budget_exceeded"
	StructuredResultNotFound        Code = "structured_result_not_found"
	TelemetryUnavailable            Code = "telemetry_unavailable"
	TelemetryPartial                Code = "telemetry_partial"
	TelemetryBudgetExceeded         Code = "telemetry_budget_exceeded"
	TelemetryIncompatibleHistory    Code = "telemetry_incompatible_history"
	ResourceObservationUnavailable  Code = "resource_observation_unavailable"
	ResourceObservationPartial      Code = "resource_observation_partial"
	ResourceLimitUnsupported        Code = "resource_limit_unsupported"
	ReproNotFound                   Code = "repro_not_found"
	ReproMaterializationUnavailable Code = "repro_materialization_unavailable"
	ReproSourceUnavailable          Code = "repro_source_unavailable"
	ReproReferenceCompacted         Code = "repro_reference_compacted"
	CapacityExceeded                Code = "capacity_exceeded"
	PersistenceUnavailable          Code = "persistence_unavailable"
	StorageReserveExhausted         Code = "storage_reserve_exhausted"
	Internal                        Code = "internal"
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
	InvalidInput:                    {message: "invalid input", details: keys("field", "reason")},
	FeatureUnavailable:              {message: "feature unavailable", details: keys("feature", "required_version")},
	OperationConflict:               {message: "operation conflicts with an existing intent", details: keys("operation_id")},
	OperationMetadataConflict:       {message: "operation metadata conflicts with existing metadata", details: keys("operation_id", "field")},
	WorkspaceNotFound:               {message: "workspace not found", details: keys("workspace_id")},
	ActivityNotFound:                {message: "activity not found", details: keys("activity_id")},
	WorkspaceAddressEscape:          {message: "workspace address escapes registered root", details: keys("workspace_id", "cwd")},
	ManifestInvalid:                 {message: "project manifest is invalid", details: keys("field", "reason")},
	ManifestReviewRequired:          {message: "project manifest review required", details: keys("manifest_version", "reason")},
	ProjectReadinessUnavailable:     {message: "project readiness unavailable", retryable: true, details: keys("workspace_id", "reason")},
	ProjectRequirementInvalid:       {message: "project requirement invalid", details: keys("requirement", "kind", "reason")},
	ToolchainMissing:                {message: "required toolchain missing", details: keys("toolchain")},
	ToolchainVersionUnknown:         {message: "toolchain version unknown", retryable: true, details: keys("toolchain", "reason")},
	ToolchainIncompatible:           {message: "toolchain incompatible", details: keys("toolchain", "reason")},
	ProjectCommandNotFound:          {message: "project command not found", details: keys("command")},
	ProjectCommandNotParameterized:  {message: "project command is not parameterized", details: keys("command")},
	ParameterUnknown:                {message: "project command parameter unknown", details: keys("command", "parameter")},
	ParameterMissing:                {message: "project command parameter missing", details: keys("command", "parameter")},
	ParameterInvalid:                {message: "project command parameter invalid", details: keys("command", "parameter", "kind", "reason")},
	ParameterKindUnsupported:        {message: "project command parameter kind unsupported", details: keys("command", "parameter", "kind")},
	ParameterValidationUnavailable:  {message: "project command parameter validation unavailable", retryable: true, details: keys("command", "parameter", "kind", "provider", "reason")},
	ProjectCommandBindingConflict:   {message: "project command binding conflicts with admitted operation", details: keys("operation_id", "command")},
	OutputCompacted:                 {message: "retained output has been compacted", details: keys("session_id")},
	OutputUnavailable:               {message: "retained output unavailable", retryable: true, details: keys("session_id", "reason")},
	OutputOutOfRange:                {message: "output selector is out of range", details: keys("session_id", "reason")},
	OutputContinuationInvalid:       {message: "output continuation is invalid", details: keys("reason")},
	OutputContinuationExpired:       {message: "output continuation has expired", details: keys("reason")},
	IdentityObservationFailed:       {message: "identity observation failed", retryable: true, details: keys("provider", "reason")},
	EventCursorInvalid:              {message: "event cursor is invalid", details: keys("reason")},
	EventCursorExpired:              {message: "event cursor has expired", details: keys("reason")},
	EvidenceCursorInvalid:           {message: "evidence cursor is invalid", details: keys("reason")},
	EvidenceCursorExpired:           {message: "evidence cursor has expired", details: keys("reason")},
	EventContinuityUnavailable:      {message: "event continuity unavailable", retryable: true, details: keys("reason")},
	StructuredAdapterUnavailable:    {message: "structured adapter unavailable", retryable: true, details: keys("adapter", "reason")},
	StructuredAdapterUnsupported:    {message: "structured adapter unsupported", details: keys("adapter")},
	StructuredResultMalformed:       {message: "structured result malformed", details: keys("adapter", "reason")},
	StructuredResultPartial:         {message: "structured result partial", details: keys("adapter", "reason")},
	StructuredResultBudgetExceeded:  {message: "structured result budget exceeded", retryable: true, details: keys("adapter", "reason")},
	StructuredResultNotFound:        {message: "structured result not found", details: keys("operation_id", "derivation_key")},
	TelemetryUnavailable:            {message: "execution telemetry unavailable", retryable: true, details: keys("operation_id", "reason")},
	TelemetryPartial:                {message: "execution telemetry partial", details: keys("operation_id", "reason")},
	TelemetryBudgetExceeded:         {message: "execution telemetry budget exceeded", retryable: true, details: keys("operation_id", "reason")},
	TelemetryIncompatibleHistory:    {message: "execution telemetry history incompatible", details: keys("operation_id", "reason")},
	ResourceObservationUnavailable:  {message: "resource observation unavailable", details: keys("metric", "reason")},
	ResourceObservationPartial:      {message: "resource observation partial", details: keys("metric", "reason")},
	ResourceLimitUnsupported:        {message: "resource limit unsupported", details: keys("metric", "reason")},
	ReproNotFound:                   {message: "reproduction capsule not found", details: keys("repro_id")},
	ReproMaterializationUnavailable: {message: "reproduction materialization unavailable", retryable: true, details: keys("operation_id", "reason")},
	ReproSourceUnavailable:          {message: "reproduction source unavailable", details: keys("repro_id", "reason")},
	ReproReferenceCompacted:         {message: "reproduction reference compacted", details: keys("repro_id", "ref_id")},
	CapacityExceeded:                {message: "capacity exceeded", retryable: true},
	PersistenceUnavailable:          {message: "persistence unavailable", retryable: true},
	StorageReserveExhausted:         {message: "storage reserve exhausted", retryable: true},
	Internal:                        {message: "internal error"},
}

var legacyCodes = map[string]Code{
	"operation_conflict":           OperationConflict,
	"operation_metadata_conflict":  OperationMetadataConflict,
	"capacity_exceeded":            CapacityExceeded,
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
