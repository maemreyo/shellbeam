package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func (i Intent) validatePersistent() error {
	if !i.Persistent {
		if i.SessionName != "" {
			return fmt.Errorf("session name requires persistent execution")
		}
		return nil
	}
	if i.TTY {
		return fmt.Errorf("persistent tty unsupported")
	}
	if i.SessionName != "" {
		if err := persistentsession.ValidateSessionName(i.SessionName); err != nil {
			return err
		}
	}
	return nil
}

func (i Intent) persistentRequestFingerprint(mode ExecutionMode, logicalCWD string) (string, error) {
	data, err := json.Marshal(struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind"`
		Mode        ExecutionMode `json:"mode"`
		Command     string        `json:"command,omitempty"`
		Argv        []string      `json:"argv,omitempty"`
		WorkspaceID string        `json:"workspace_id,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Persistent  bool          `json:"persistent"`
		SessionName string        `json:"session_name,omitempty"`
	}{
		Version: 4, Kind: "request", Mode: mode, Command: i.Command, Argv: i.Argv,
		WorkspaceID: i.WorkspaceID, CWD: logicalCWD, TTY: i.TTY, TimeoutMS: i.TimeoutMS,
		Persistent: true, SessionName: i.SessionName,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (i Intent) persistentExecutionFingerprint(mode ExecutionMode, cwd, executable string) (string, error) {
	data, err := json.Marshal(struct {
		Version    int           `json:"version"`
		Kind       string        `json:"kind"`
		Mode       ExecutionMode `json:"mode"`
		Command    string        `json:"command,omitempty"`
		Argv       []string      `json:"argv,omitempty"`
		CWD        string        `json:"cwd"`
		TTY        bool          `json:"tty"`
		TimeoutMS  int64         `json:"timeout_ms"`
		Executable string        `json:"executable"`
		Persistent bool          `json:"persistent"`
	}{
		Version: 4, Kind: "execution", Mode: mode, Command: i.Command, Argv: i.Argv,
		CWD: cwd, TTY: i.TTY, TimeoutMS: i.TimeoutMS, Executable: executable, Persistent: true,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
