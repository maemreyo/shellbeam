package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	delegatedsession "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func (i Intent) validateSessionContract() error {
	return validateSessionContract(i.SessionMode, i.TTY, i.Persistent, i.SessionName)
}

func validateSessionContract(sessionMode string, tty, persistent bool, sessionName string) error {
	if sessionMode == "" {
		if !persistent {
			if sessionName != "" {
				return fmt.Errorf("session name requires persistent execution")
			}
			return nil
		}
		if tty {
			return fmt.Errorf("persistent tty unsupported")
		}
		if sessionName != "" {
			return persistentsession.ValidateSessionName(sessionName)
		}
		return nil
	}
	if err := delegatedsession.ValidateMode(sessionMode); err != nil {
		return err
	}
	if tty {
		return fmt.Errorf("delegated interactive tty legacy field unsupported")
	}
	if persistent {
		return fmt.Errorf("delegated interactive persistent legacy field unsupported")
	}
	if sessionName != "" {
		if err := persistentsession.ValidateSessionName(sessionName); err != nil {
			return err
		}
	}
	return nil
}

func (i Intent) delegatedRequestFingerprint(mode ExecutionMode, logicalCWD string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind"`
		SessionMode string        `json:"session_mode"`
		Mode        ExecutionMode `json:"mode"`
		Command     string        `json:"command,omitempty"`
		Argv        []string      `json:"argv,omitempty"`
		WorkspaceID string        `json:"workspace_id,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Persistent  bool          `json:"persistent"`
		SessionName string        `json:"session_name,omitempty"`
		Policy      *policyDigest `json:"policy,omitempty"`
	}{
		Version: 1, Kind: "delegated_request", SessionMode: i.SessionMode, Mode: mode,
		Command: i.Command, Argv: i.Argv, WorkspaceID: i.WorkspaceID, CWD: logicalCWD,
		TTY: i.TTY, TimeoutMS: i.TimeoutMS, Persistent: i.Persistent, SessionName: i.SessionName,
		Policy: i.requestPolicy(),
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func (i Intent) delegatedExecutionFingerprint(mode ExecutionMode, cwd, executable string) (string, error) {
	encoded, err := json.Marshal(struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind"`
		SessionMode string        `json:"session_mode"`
		Mode        ExecutionMode `json:"mode"`
		Command     string        `json:"command,omitempty"`
		Argv        []string      `json:"argv,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Executable  string        `json:"executable"`
		Persistent  bool          `json:"persistent"`
		SessionName string        `json:"session_name,omitempty"`
		Policy      *policyDigest `json:"policy,omitempty"`
	}{
		Version: 1, Kind: "delegated_execution", SessionMode: i.SessionMode, Mode: mode,
		Command: i.Command, Argv: i.Argv, CWD: cwd, TTY: i.TTY, TimeoutMS: i.TimeoutMS,
		Executable: executable, Persistent: i.Persistent, SessionName: i.SessionName,
		Policy: i.executionPolicy(),
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func (i TypedRequestIntent) delegatedTypedRequestFingerprint(params []typedParam) (string, error) {
	encoded, err := json.Marshal(struct {
		Version          int          `json:"version"`
		Kind             string       `json:"kind"`
		SessionMode      string       `json:"session_mode"`
		WorkspaceID      string       `json:"workspace_id"`
		ProjectCommandID string       `json:"project_command_id"`
		Params           []typedParam `json:"params"`
		TTY              bool         `json:"tty"`
		TimeoutMS        int64        `json:"timeout_ms"`
		Persistent       bool         `json:"persistent"`
		SessionName      string       `json:"session_name,omitempty"`
	}{
		Version: 1, Kind: "delegated_typed_request", SessionMode: i.SessionMode,
		WorkspaceID: i.WorkspaceID, ProjectCommandID: i.ProjectCommandID, Params: params,
		TTY: i.TTY, TimeoutMS: i.TimeoutMS, Persistent: i.Persistent, SessionName: i.SessionName,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
