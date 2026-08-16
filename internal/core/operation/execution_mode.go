package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type ExecutionMode string

const (
	ExecutionModeShell ExecutionMode = "shell"
	ExecutionModeArgv  ExecutionMode = "argv"
)

type IntentKind string

const (
	IntentKindInspect     IntentKind = "inspect"
	IntentKindFormat      IntentKind = "format"
	IntentKindEdit        IntentKind = "edit"
	IntentKindTest        IntentKind = "test"
	IntentKindBuild       IntentKind = "build"
	IntentKindGenerate    IntentKind = "generate"
	IntentKindGitPush     IntentKind = "git-push"
	IntentKindRelease     IntentKind = "release"
	IntentKindLongRunning IntentKind = "long-running"
	IntentKindOther       IntentKind = "other"
)

type DeclaredIntent struct {
	Kind           IntentKind `json:"kind"`
	MutatesSource  *bool      `json:"mutates_source,omitempty"`
	ExternalEffect *bool      `json:"external_effect,omitempty"`
}

func (i DeclaredIntent) Validate() error {
	switch i.Kind {
	case IntentKindInspect, IntentKindFormat, IntentKindEdit, IntentKindTest, IntentKindBuild, IntentKindGenerate, IntentKindGitPush, IntentKindRelease, IntentKindLongRunning, IntentKindOther:
		return nil
	default:
		return fmt.Errorf("invalid declared intent kind")
	}
}

func (i Intent) ExecutionMode() (ExecutionMode, error) {
	hasCommand := i.Command != ""
	hasArgv := i.Argv != nil
	if hasCommand == hasArgv {
		return "", fmt.Errorf("exactly one execution form required")
	}
	if hasCommand {
		return ExecutionModeShell, nil
	}
	if len(i.Argv) == 0 || i.Argv[0] == "" {
		return "", fmt.Errorf("argv executable is empty")
	}
	return ExecutionModeArgv, nil
}

func hashArgvIntent(version int, kind string, argv []string, workspaceID, cwd string, tty bool, timeoutMS int64, executable string, policy *policyDigest) (string, error) {
	data, err := json.Marshal(struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind"`
		Mode        ExecutionMode `json:"mode"`
		Argv        []string      `json:"argv"`
		WorkspaceID string        `json:"workspace_id,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Executable  string        `json:"executable,omitempty"`
		Policy      *policyDigest `json:"policy,omitempty"`
	}{version, kind, ExecutionModeArgv, argv, workspaceID, cwd, tty, timeoutMS, executable, policy})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
