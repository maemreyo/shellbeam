package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	TypedIntentClaimSchemaVersion = 1
	maxTypedParams                = 32
	maxTypedParamValueBytes       = 1024
)

var typedProjectIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type TypedRequestIntent struct {
	WorkspaceID      string            `json:"workspace_id"`
	ProjectCommandID string            `json:"project_command_id"`
	Params           map[string]string `json:"params,omitempty"`
	TTY              bool              `json:"tty"`
	TimeoutMS        int64             `json:"timeout_ms"`
	Persistent       bool              `json:"persistent,omitempty"`
	SessionMode      string            `json:"session_mode,omitempty"`
	SessionName      string            `json:"session_name,omitempty"`
	StdinMode        StdinMode         `json:"stdin_mode,omitempty"`
	TimeoutMode      TimeoutMode       `json:"timeout_mode,omitempty"`
	TraceMode        trace.Mode        `json:"trace_mode,omitempty"`
	ResourceLimits   *ResourceLimits   `json:"resource_limits,omitempty"`
}

type TypedIntentClaim struct {
	SchemaVersion      int                `json:"schema_version"`
	OperationID        ID                 `json:"operation_id"`
	RequestFingerprint string             `json:"request_fingerprint"`
	Intent             TypedRequestIntent `json:"intent"`
	CreatedAt          time.Time          `json:"created_at"`
}

type typedParam struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func (i TypedRequestIntent) Validate() error {
	if _, err := workspace.ParseWorkspaceID(i.WorkspaceID); err != nil {
		return err
	}
	if !typedProjectIDPattern.MatchString(i.ProjectCommandID) {
		return fmt.Errorf("invalid project command id")
	}
	if i.TimeoutMS < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	if err := i.StdinMode.Validate(); err != nil {
		return err
	}
	if err := i.TimeoutMode.Validate(); err != nil {
		return err
	}
	if _, err := trace.NormalizeMode(i.TraceMode); err != nil {
		return err
	}
	if i.ResourceLimits != nil {
		if err := i.ResourceLimits.Validate(); err != nil {
			return err
		}
	}
	if err := validateSessionContract(i.SessionMode, i.TTY, i.Persistent, i.SessionName); err != nil {
		return err
	}
	if len(i.Params) > maxTypedParams {
		return fmt.Errorf("typed parameter limit exceeded")
	}
	for id, value := range i.Params {
		if !typedProjectIDPattern.MatchString(id) || !validTypedParamValue(value) {
			return fmt.Errorf("invalid typed parameter")
		}
	}
	return nil
}

func (i TypedRequestIntent) Fingerprint() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	params := make([]typedParam, 0, len(i.Params))
	for id, value := range i.Params {
		params = append(params, typedParam{ID: id, Value: value})
	}
	sort.Slice(params, func(a, b int) bool { return params[a].ID < params[b].ID })
	if i.SessionMode != "" {
		base, err := i.delegatedTypedRequestFingerprint(params)
		if err != nil {
			return "", err
		}
		base, err = bindResourceFingerprint("request", base, i.ResourceLimits)
		if err != nil {
			return "", err
		}
		return bindTraceRequestFingerprint(base, i.TraceMode)
	}
	var payload any
	if i.Persistent {
		payload = struct {
			Version          int          `json:"version"`
			WorkspaceID      string       `json:"workspace_id"`
			ProjectCommandID string       `json:"project_command_id"`
			Params           []typedParam `json:"params"`
			TTY              bool         `json:"tty"`
			TimeoutMS        int64        `json:"timeout_ms"`
			Persistent       bool         `json:"persistent"`
			SessionName      string       `json:"session_name,omitempty"`
		}{2, i.WorkspaceID, i.ProjectCommandID, params, i.TTY, i.TimeoutMS, true, i.SessionName}
	} else {
		payload = struct {
			Version          int          `json:"version"`
			WorkspaceID      string       `json:"workspace_id"`
			ProjectCommandID string       `json:"project_command_id"`
			Params           []typedParam `json:"params"`
			TTY              bool         `json:"tty"`
			TimeoutMS        int64        `json:"timeout_ms"`
		}{1, i.WorkspaceID, i.ProjectCommandID, params, i.TTY, i.TimeoutMS}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	base := hex.EncodeToString(sum[:])
	if policy := RequestPolicyDigest(i.StdinMode, i.TimeoutMode); policy != nil {
		encodedPolicy, policyErr := json.Marshal(struct {
			Version int           `json:"version"`
			Base    string        `json:"base"`
			Policy  *policyDigest `json:"policy"`
		}{1, base, policy})
		if policyErr != nil {
			return "", policyErr
		}
		sum = sha256.Sum256(encodedPolicy)
		base = hex.EncodeToString(sum[:])
	}
	base, err = bindResourceFingerprint("request", base, i.ResourceLimits)
	if err != nil {
		return "", err
	}
	return bindTraceRequestFingerprint(base, i.TraceMode)
}

func (c TypedIntentClaim) Validate() error {
	if c.SchemaVersion != TypedIntentClaimSchemaVersion {
		return fmt.Errorf("unsupported typed intent claim schema")
	}
	if _, err := ParseID(string(c.OperationID)); err != nil {
		return err
	}
	if c.CreatedAt.IsZero() {
		return fmt.Errorf("typed intent claim created_at missing")
	}
	fingerprint, err := c.Intent.Fingerprint()
	if err != nil {
		return err
	}
	if !validTypedDigest(c.RequestFingerprint) || fingerprint != c.RequestFingerprint {
		return fmt.Errorf("typed intent claim fingerprint mismatch")
	}
	return nil
}

func validTypedParamValue(value string) bool {
	if value == "" || len(value) > maxTypedParamValueBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validTypedDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
