package supervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	BootstrapSchemaVersion = 1
	MaxBootstrapBytes      = 32 << 10
)

type BootstrapExecution struct {
	Mode       operation.ExecutionMode `json:"mode"`
	Shell      string                  `json:"shell,omitempty"`
	Executable string                  `json:"executable"`
	Command    string                  `json:"command,omitempty"`
	Argv       []string                `json:"argv,omitempty"`
	CWD        string                  `json:"cwd"`
	TTY        bool                    `json:"tty"`
	TimeoutMS  int64                   `json:"timeout_ms"`
}

type Bootstrap struct {
	SchemaVersion         int                `json:"schema_version"`
	RuntimeRoot           string             `json:"runtime_root"`
	SessionID             string             `json:"session_id"`
	GenerationID          string             `json:"generation_id"`
	Execution             BootstrapExecution `json:"execution"`
	MaxOutputBytes        int64              `json:"max_output_bytes"`
	MaxQueuedInputBytes   int                `json:"max_queued_input_bytes"`
	MaxInputRecords       int                `json:"max_input_records"`
	MaxInputMetadataBytes int                `json:"max_input_metadata_bytes"`
	MaxKillRecords        int                `json:"max_kill_records"`
	TerminationGraceMS    int64              `json:"termination_grace_ms"`
}

func (b Bootstrap) Validate() error {
	if b.SchemaVersion != BootstrapSchemaVersion || !filepath.IsAbs(b.RuntimeRoot) {
		return fmt.Errorf("invalid supervisor bootstrap")
	}
	if _, err := operation.ParseSessionID(b.SessionID); err != nil || !validOpaque(b.GenerationID) {
		return fmt.Errorf("invalid supervisor bootstrap identity")
	}
	if b.MaxOutputBytes < 1 || b.MaxQueuedInputBytes < 1 || b.MaxInputRecords < 1 || b.MaxInputRecords > 4096 || b.MaxInputMetadataBytes < 256 || b.MaxInputMetadataBytes > 1<<20 || b.MaxKillRecords < 1 || b.MaxKillRecords > 256 || b.TerminationGraceMS < 0 {
		return fmt.Errorf("invalid supervisor bootstrap limits")
	}
	e := b.Execution
	if e.TTY || e.TimeoutMS < 0 || !filepath.IsAbs(e.CWD) || !filepath.IsAbs(e.Executable) {
		return fmt.Errorf("invalid supervisor bootstrap execution")
	}
	switch e.Mode {
	case operation.ExecutionModeShell:
		if e.Shell == "" || e.Command == "" || len(e.Argv) != 0 || e.Executable != e.Shell {
			return fmt.Errorf("invalid supervisor shell bootstrap")
		}
	case operation.ExecutionModeArgv:
		if len(e.Argv) == 0 || e.Argv[0] == "" || e.Command != "" || e.Shell != "" || e.Executable == "" {
			return fmt.Errorf("invalid supervisor argv bootstrap")
		}
	default:
		return fmt.Errorf("invalid supervisor execution mode")
	}
	return nil
}

func (b Bootstrap) ExecutionSpec() operation.ExecutionSpec {
	return operation.ExecutionSpec{
		Mode: b.Execution.Mode, Shell: b.Execution.Shell, Executable: b.Execution.Executable,
		Command: b.Execution.Command, Argv: append([]string(nil), b.Execution.Argv...), CWD: b.Execution.CWD,
		TTY: b.Execution.TTY, TimeoutMS: b.Execution.TimeoutMS,
	}
}

func EncodeBootstrap(writer io.Writer, bootstrap Bootstrap) error {
	if err := bootstrap.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(bootstrap)
	if err != nil {
		return err
	}
	if len(encoded) > MaxBootstrapBytes {
		return fmt.Errorf("supervisor bootstrap exceeds limit")
	}
	written := 0
	for written < len(encoded) {
		n, err := writer.Write(encoded[written:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		written += n
	}
	return nil
}

func DecodeBootstrap(reader io.Reader) (Bootstrap, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaxBootstrapBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > MaxBootstrapBytes {
		return Bootstrap{}, fmt.Errorf("invalid supervisor bootstrap")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var bootstrap Bootstrap
	if err := decoder.Decode(&bootstrap); err != nil {
		return Bootstrap{}, fmt.Errorf("invalid supervisor bootstrap")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Bootstrap{}, fmt.Errorf("invalid supervisor bootstrap")
	}
	if err := bootstrap.Validate(); err != nil {
		return Bootstrap{}, err
	}
	return bootstrap, nil
}
