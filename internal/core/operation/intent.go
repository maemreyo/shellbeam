package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Intent struct {
	Command        string   `json:"command,omitempty"`
	Argv           []string `json:"argv,omitempty"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	CWD            string   `json:"cwd"`
	ResolvedCWD    string   `json:"-"`
	TTY            bool     `json:"tty"`
	TimeoutMS      int64    `json:"timeout_ms"`
	Persistent     bool     `json:"persistent,omitempty"`
	SessionName    string   `json:"session_name,omitempty"`
	YieldMS        int64    `json:"-"`
	MaxOutputBytes int      `json:"-"`
	// StdinMode and TimeoutMode are the caller's raw words, kept unresolved so
	// the request fingerprint can describe the request rather than the policy
	// version that happened to be in force.
	StdinMode            StdinMode             `json:"stdin_mode,omitempty"`
	TimeoutMode          TimeoutMode           `json:"timeout_mode,omitempty"`
	TraceMode            trace.Mode            `json:"-"`
	TraceExecutionDigest string                `json:"-"`
	ResourceLimits       *ResourceLimits       `json:"-"`
	Hermetic             *hermeticcore.Request `json:"-"`
	// Resolved and TimeoutSource are the contract the daemon settled on; they
	// belong to the execution fingerprint only.
	Resolved      *ResolvedExecutionPolicy `json:"-"`
	TimeoutSource string                   `json:"-"`
}

func (i Intent) Fingerprint() (string, error) {
	if i.WorkspaceID != "" {
		return "", fmt.Errorf("workspace addressing requires v2")
	}
	mode, err := i.ExecutionMode()
	if err != nil {
		return "", err
	}
	if mode != ExecutionModeShell {
		return "", fmt.Errorf("argv execution requires v2")
	}
	if err := i.validateCommon(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(i.CWD) {
		return "", fmt.Errorf("cwd must be absolute")
	}
	traceMode, err := trace.NormalizeMode(i.TraceMode)
	if err != nil {
		return "", err
	}
	if traceMode != trace.ModeOff {
		return "", fmt.Errorf("input tracing requires v2")
	}
	if i.ResourceLimits != nil {
		return "", fmt.Errorf("resource limits require v2")
	}
	if i.Hermetic != nil {
		return "", fmt.Errorf("hermetic execution requires v2")
	}
	return hashIntent(1, "request", i.Command, "", i.CWD, i.TTY, i.TimeoutMS, "", nil)
}

func (i Intent) RequestFingerprint() (string, error) {
	mode, err := i.ExecutionMode()
	if err != nil {
		return "", err
	}
	if err := i.validateCommon(); err != nil {
		return "", err
	}
	address := workspace.Address{WorkspaceID: workspace.WorkspaceID(i.WorkspaceID), CWD: i.CWD}
	if err := address.Validate(); err != nil {
		return "", err
	}
	logicalCWD := address.LogicalCWD()
	var base string
	if i.Persistent {
		base, err = i.persistentRequestFingerprint(mode, logicalCWD)
	} else if mode == ExecutionModeShell {
		base, err = hashIntent(2, "request", i.Command, i.WorkspaceID, logicalCWD, i.TTY, i.TimeoutMS, "", i.requestPolicy())
	} else {
		base, err = hashArgvIntent(3, "request", i.Argv, i.WorkspaceID, logicalCWD, i.TTY, i.TimeoutMS, "", i.requestPolicy())
	}
	if err != nil {
		return "", err
	}
	base, err = bindResourceFingerprint("request", base, i.ResourceLimits)
	if err != nil {
		return "", err
	}
	base, err = hermeticcore.BindFingerprint("request", base, i.Hermetic)
	if err != nil {
		return "", err
	}
	return bindTraceRequestFingerprint(base, i.TraceMode)
}

func (i Intent) ExecutionFingerprint(effectiveExecutable string) (string, error) {
	if effectiveExecutable == "" {
		return "", fmt.Errorf("effective executable is empty")
	}
	mode, err := i.ExecutionMode()
	if err != nil {
		return "", err
	}
	if err := i.validateCommon(); err != nil {
		return "", err
	}
	cwd := i.ResolvedCWD
	if cwd == "" {
		cwd = i.CWD
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("resolved cwd must be absolute")
	}
	var base string
	if i.Persistent {
		base, err = i.persistentExecutionFingerprint(mode, cwd, effectiveExecutable)
	} else if mode == ExecutionModeShell {
		base, err = hashIntent(2, "execution", i.Command, "", cwd, i.TTY, i.TimeoutMS, effectiveExecutable, i.executionPolicy())
	} else {
		base, err = hashArgvIntent(3, "execution", i.Argv, "", cwd, i.TTY, i.TimeoutMS, effectiveExecutable, i.executionPolicy())
	}
	if err != nil {
		return "", err
	}
	base, err = bindResourceFingerprint("execution", base, i.ResourceLimits)
	if err != nil {
		return "", err
	}
	base, err = hermeticcore.BindFingerprint("execution", base, i.Hermetic)
	if err != nil {
		return "", err
	}
	return bindTraceExecutionFingerprint(base, i.TraceMode, i.TraceExecutionDigest)
}

// requestPolicy carries only what the caller named, so a request that named
// nothing hashes exactly as it did before these settings existed.
func (i Intent) requestPolicy() *policyDigest {
	return RequestPolicyDigest(i.StdinMode, i.TimeoutMode)
}

// executionPolicy carries the contract that will actually run. It is absent
// only when nothing resolved it, which is the case for callers on the protocol
// version that predates these settings.
func (i Intent) executionPolicy() *policyDigest {
	if i.Resolved == nil {
		return nil
	}
	return ExecutionPolicyDigest(*i.Resolved, i.TimeoutSource)
}

func (i Intent) validateCommon() error {
	if _, err := i.ExecutionMode(); err != nil {
		return err
	}
	if i.TimeoutMS < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	traceMode, err := trace.NormalizeMode(i.TraceMode)
	if err != nil {
		return err
	}
	if traceMode == trace.ModeOff && i.TraceExecutionDigest != "" {
		return fmt.Errorf("trace execution digest requires active trace mode")
	}
	if i.TraceExecutionDigest != "" && !validTraceExecutionDigest(i.TraceExecutionDigest) {
		return fmt.Errorf("invalid trace execution digest")
	}
	if i.ResourceLimits != nil {
		if err := i.ResourceLimits.Validate(); err != nil {
			return err
		}
	}
	if i.Hermetic != nil {
		if err := i.Hermetic.Validate(); err != nil {
			return err
		}
		if i.TTY || i.Persistent || (i.StdinMode != "" && i.StdinMode != StdinModeClosed) {
			return fmt.Errorf("hermetic v1 requires non-tty, non-persistent, closed stdin")
		}
	}
	if err := i.validatePersistent(); err != nil {
		return err
	}
	return nil
}

func hashIntent(version int, kind, command, workspaceID, cwd string, tty bool, timeoutMS int64, shell string, policy *policyDigest) (string, error) {
	b, err := json.Marshal(struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind,omitempty"`
		Command     string        `json:"command"`
		WorkspaceID string        `json:"workspace_id,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Shell       string        `json:"shell,omitempty"`
		Policy      *policyDigest `json:"policy,omitempty"`
	}{version, kind, command, workspaceID, cwd, tty, timeoutMS, shell, policy})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func bindTraceRequestFingerprint(base string, mode trace.Mode) (string, error) {
	normalized, err := trace.NormalizeMode(mode)
	if err != nil {
		return "", err
	}
	if normalized == trace.ModeOff {
		return base, nil
	}
	return hashTraceEnvelope("request", base, normalized, "")
}

func BindInputTraceExecutionFingerprint(base string, mode trace.Mode, digest string) (string, error) {
	if !validTraceExecutionDigest(base) {
		return "", fmt.Errorf("invalid base execution fingerprint")
	}
	return bindTraceExecutionFingerprint(base, mode, digest)
}

func bindTraceExecutionFingerprint(base string, mode trace.Mode, digest string) (string, error) {
	normalized, err := trace.NormalizeMode(mode)
	if err != nil {
		return "", err
	}
	if normalized == trace.ModeOff {
		if digest != "" {
			return "", fmt.Errorf("trace execution digest requires active trace mode")
		}
		return base, nil
	}
	if digest == "" {
		if normalized == trace.ModeBestEffort {
			return base, nil
		}
		return "", fmt.Errorf("required input tracing lacks instrumentation binding")
	}
	if !validTraceExecutionDigest(digest) {
		return "", fmt.Errorf("invalid trace execution digest")
	}
	return hashTraceEnvelope("execution", base, normalized, digest)
}

func hashTraceEnvelope(kind, base string, mode trace.Mode, digest string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version               int        `json:"version"`
		Kind                  string     `json:"kind"`
		Base                  string     `json:"base_fingerprint"`
		Mode                  trace.Mode `json:"trace_mode"`
		InstrumentationDigest string     `json:"instrumentation_digest,omitempty"`
	}{Version: 1, Kind: kind, Base: base, Mode: mode, InstrumentationDigest: digest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validTraceExecutionDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type ObservationBinding struct {
	ActivityID        string             `json:"activity_id,omitempty"`
	Intent            *DeclaredIntent    `json:"intent,omitempty"`
	StructuredAdapter string             `json:"structured_adapter,omitempty"`
	Evidence          *evidence.Contract `json:"evidence,omitempty"`
}

func ValidStructuredAdapterID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}

func (b ObservationBinding) Fingerprint() (string, error) {
	if b.StructuredAdapter != "" && !ValidStructuredAdapterID(b.StructuredAdapter) {
		return "", fmt.Errorf("invalid structured adapter")
	}
	if b.Evidence != nil {
		normalized, err := b.Evidence.Normalize()
		if err != nil {
			return "", err
		}
		if b.Intent != nil {
			if err := b.Intent.Validate(); err != nil {
				return "", err
			}
		}
		data, err := json.Marshal(struct {
			Version           int               `json:"version"`
			ActivityID        string            `json:"activity_id,omitempty"`
			Intent            *DeclaredIntent   `json:"intent,omitempty"`
			StructuredAdapter string            `json:"structured_adapter,omitempty"`
			Evidence          evidence.Contract `json:"evidence"`
		}{Version: 4, ActivityID: b.ActivityID, Intent: b.Intent, StructuredAdapter: b.StructuredAdapter, Evidence: normalized})
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	if b.StructuredAdapter == "" {
		if b.Intent == nil {
			if b.ActivityID == "" {
				return "", nil
			}
			data, err := json.Marshal(struct {
				Version    int    `json:"version"`
				ActivityID string `json:"activity_id,omitempty"`
			}{Version: 1, ActivityID: b.ActivityID})
			if err != nil {
				return "", err
			}
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:]), nil
		}
		if err := b.Intent.Validate(); err != nil {
			return "", err
		}
		data, err := json.Marshal(struct {
			Version    int            `json:"version"`
			ActivityID string         `json:"activity_id,omitempty"`
			Intent     DeclaredIntent `json:"intent"`
		}{Version: 2, ActivityID: b.ActivityID, Intent: *b.Intent})
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	if b.Intent != nil {
		if err := b.Intent.Validate(); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(struct {
		Version           int             `json:"version"`
		ActivityID        string          `json:"activity_id,omitempty"`
		Intent            *DeclaredIntent `json:"intent,omitempty"`
		StructuredAdapter string          `json:"structured_adapter"`
	}{Version: 3, ActivityID: b.ActivityID, Intent: b.Intent, StructuredAdapter: b.StructuredAdapter})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
