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
	Command        string `json:"command"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	CWD            string `json:"cwd"`
	ResolvedCWD    string `json:"-"`
	TTY            bool   `json:"tty"`
	TimeoutMS      int64  `json:"timeout_ms"`
	YieldMS        int64  `json:"-"`
	MaxOutputBytes int    `json:"-"`
}

func (i Intent) Fingerprint() (string, error) {
	if i.WorkspaceID != "" {
		return "", fmt.Errorf("workspace addressing requires v2")
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
	if err := i.validateCommon(); err != nil {
		return "", err
	}
	address := workspace.Address{WorkspaceID: workspace.WorkspaceID(i.WorkspaceID), CWD: i.CWD}
	if err := address.Validate(); err != nil {
		return "", err
	}
	return hashIntent(2, "request", i.Command, i.WorkspaceID, address.LogicalCWD(), i.TTY, i.TimeoutMS, "")
}

func (i Intent) ExecutionFingerprint(shell string) (string, error) {
	if shell == "" {
		return "", fmt.Errorf("shell is empty")
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
	return hashIntent(2, "execution", i.Command, "", cwd, i.TTY, i.TimeoutMS, shell)
}

func (i Intent) validateCommon() error {
	if i.Command == "" {
		return fmt.Errorf("command is empty")
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
	ActivityID string `json:"activity_id,omitempty"`
}

func (b ObservationBinding) Fingerprint() (string, error) {
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
