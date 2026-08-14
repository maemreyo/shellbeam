package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"

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
	YieldMS        int64    `json:"-"`
	MaxOutputBytes int      `json:"-"`
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
	return hashIntent(1, "request", i.Command, "", i.CWD, i.TTY, i.TimeoutMS, "")
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
	if mode == ExecutionModeShell {
		return hashIntent(2, "request", i.Command, i.WorkspaceID, logicalCWD, i.TTY, i.TimeoutMS, "")
	}
	return hashArgvIntent(3, "request", i.Argv, i.WorkspaceID, logicalCWD, i.TTY, i.TimeoutMS, "")
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
	if mode == ExecutionModeShell {
		return hashIntent(2, "execution", i.Command, "", cwd, i.TTY, i.TimeoutMS, effectiveExecutable)
	}
	return hashArgvIntent(3, "execution", i.Argv, "", cwd, i.TTY, i.TimeoutMS, effectiveExecutable)
}

func (i Intent) validateCommon() error {
	if _, err := i.ExecutionMode(); err != nil {
		return err
	}
	if i.TimeoutMS < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	return nil
}

func hashIntent(version int, kind, command, workspaceID, cwd string, tty bool, timeoutMS int64, shell string) (string, error) {
	b, err := json.Marshal(struct {
		Version     int    `json:"version"`
		Kind        string `json:"kind,omitempty"`
		Command     string `json:"command"`
		WorkspaceID string `json:"workspace_id,omitempty"`
		CWD         string `json:"cwd"`
		TTY         bool   `json:"tty"`
		TimeoutMS   int64  `json:"timeout_ms"`
		Shell       string `json:"shell,omitempty"`
	}{version, kind, command, workspaceID, cwd, tty, timeoutMS, shell})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type ObservationBinding struct {
	ActivityID        string          `json:"activity_id,omitempty"`
	Intent            *DeclaredIntent `json:"intent,omitempty"`
	StructuredAdapter string          `json:"structured_adapter,omitempty"`
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
