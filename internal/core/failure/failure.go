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
	InvalidInput              Code = "invalid_input"
	FeatureUnavailable        Code = "feature_unavailable"
	OperationConflict         Code = "operation_conflict"
	OperationMetadataConflict Code = "operation_metadata_conflict"
	WorkspaceNotFound         Code = "workspace_not_found"
	WorkspaceAddressEscape    Code = "workspace_address_escape"
	ManifestInvalid           Code = "manifest_invalid"
	ManifestReviewRequired    Code = "manifest_review_required"
	IdentityObservationFailed Code = "identity_observation_failed"
	CapacityExceeded          Code = "capacity_exceeded"
	PersistenceUnavailable    Code = "persistence_unavailable"
	StorageReserveExhausted   Code = "storage_reserve_exhausted"
	Internal                  Code = "internal"
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
	InvalidInput:              {message: "invalid input", details: keys("field", "reason")},
	FeatureUnavailable:        {message: "feature unavailable", details: keys("feature", "required_version")},
	OperationConflict:         {message: "operation conflicts with an existing intent", details: keys("operation_id")},
	OperationMetadataConflict: {message: "operation metadata conflicts with existing metadata", details: keys("operation_id", "field")},
	WorkspaceNotFound:         {message: "workspace not found", details: keys("workspace_id")},
	WorkspaceAddressEscape:    {message: "workspace address escapes registered root", details: keys("workspace_id", "cwd")},
	ManifestInvalid:           {message: "project manifest is invalid", details: keys("field", "reason")},
	ManifestReviewRequired:    {message: "project manifest review required", details: keys("manifest_version", "reason")},
	IdentityObservationFailed: {message: "identity observation failed", retryable: true, details: keys("provider", "reason")},
	CapacityExceeded:          {message: "capacity exceeded", retryable: true},
	PersistenceUnavailable:    {message: "persistence unavailable", retryable: true},
	StorageReserveExhausted:   {message: "storage reserve exhausted", retryable: true},
	Internal:                  {message: "internal error"},
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
