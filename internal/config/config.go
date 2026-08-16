// Package config loads strict versioned per-user configuration.
package config

import (
	"fmt"
	"path/filepath"

	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
)

type Config struct {
	GitProfiles                map[string]gitidentity.Profile `toml:"git_profiles" json:"git_profiles,omitempty"`
	GitRepositoryProfiles      map[string]string              `toml:"git_repository_profiles" json:"git_repository_profiles,omitempty"`
	GitWorkspaceProfiles       map[string]string              `toml:"git_workspace_profiles" json:"git_workspace_profiles,omitempty"`
	SchemaVersion              int                            `toml:"schema_version" json:"schema_version"`
	RuntimeDir                 string                         `toml:"runtime_dir" json:"runtime_dir"`
	StateDir                   string                         `toml:"state_dir" json:"state_dir"`
	Shell                      string                         `toml:"shell" json:"shell"`
	MaxConcurrentSessions      int                            `toml:"max_concurrent_sessions" json:"max_concurrent_sessions"`
	DefaultYieldMS             int64                          `toml:"default_yield_ms" json:"default_yield_ms"`
	MaxYieldMS                 int64                          `toml:"max_yield_ms" json:"max_yield_ms"`
	DefaultMaxOutputBytes      int                            `toml:"default_max_output_bytes" json:"default_max_output_bytes"`
	MaxResponseOutputBytes     int                            `toml:"max_response_output_bytes" json:"max_response_output_bytes"`
	MaxCommandBytes            int                            `toml:"max_command_bytes" json:"max_command_bytes"`
	MaxStdinCallBytes          int                            `toml:"max_stdin_call_bytes" json:"max_stdin_call_bytes"`
	MaxQueuedInputSessionBytes int                            `toml:"max_queued_input_session_bytes" json:"max_queued_input_session_bytes"`
	MaxQueuedInputTotalBytes   int                            `toml:"max_queued_input_total_bytes" json:"max_queued_input_total_bytes"`
	MaxSessionOutputBytes      int64                          `toml:"max_session_output_bytes" json:"max_session_output_bytes"`
	MaxTotalStateBytes         int64                          `toml:"max_total_state_bytes" json:"max_total_state_bytes"`
	MinFreeSpaceBytes          int64                          `toml:"min_free_space_bytes" json:"min_free_space_bytes"`
	ControlReserveSessionBytes int64                          `toml:"control_reserve_session_bytes" json:"control_reserve_session_bytes"`
	TerminalRetentionHours     int                            `toml:"terminal_retention_hours" json:"terminal_retention_hours"`
	MaxTimeoutMS               int64                          `toml:"max_timeout_ms" json:"max_timeout_ms"`
	// DefaultTimeoutMS bounds an ordinary command whose caller named no
	// timeout. It exists because omission used to mean "run forever", which is
	// how commands that were only waiting for input held session slots for days.
	DefaultTimeoutMS   int64 `toml:"default_timeout_ms" json:"default_timeout_ms"`
	TerminationGraceMS int64 `toml:"termination_grace_ms" json:"termination_grace_ms"`
	FinalizeRetryMinMS int64 `toml:"finalize_retry_min_ms" json:"finalize_retry_min_ms"`
	FinalizeRetryMaxMS int64 `toml:"finalize_retry_max_ms" json:"finalize_retry_max_ms"`
}

func Defaults() Config {
	return Config{SchemaVersion: 1, MaxConcurrentSessions: 4, DefaultYieldMS: 10000, MaxYieldMS: 30000, DefaultMaxOutputBytes: 20000, MaxResponseOutputBytes: 262144, MaxCommandBytes: 32768, MaxStdinCallBytes: 65536, MaxQueuedInputSessionBytes: 262144, MaxQueuedInputTotalBytes: 1048576, MaxSessionOutputBytes: 268435456, MaxTotalStateBytes: 10737418240, MinFreeSpaceBytes: 536870912, ControlReserveSessionBytes: 1048576, TerminalRetentionHours: 168, MaxTimeoutMS: 86400000, DefaultTimeoutMS: 600000, TerminationGraceMS: 5000, FinalizeRetryMinMS: 100, FinalizeRetryMaxMS: 5000}
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 || c.MaxConcurrentSessions < 1 {
		return fmt.Errorf("invalid schema or concurrency")
	}
	if c.DefaultYieldMS < 0 || c.DefaultYieldMS > c.MaxYieldMS || c.MaxQueuedInputSessionBytes > c.MaxQueuedInputTotalBytes || c.MaxSessionOutputBytes+c.ControlReserveSessionBytes > c.MaxTotalStateBytes || c.MinFreeSpaceBytes < c.ControlReserveSessionBytes || c.FinalizeRetryMinMS < 1 || c.FinalizeRetryMinMS > c.FinalizeRetryMaxMS || c.DefaultTimeoutMS < 0 || (c.DefaultTimeoutMS > 0 && c.MaxTimeoutMS > 0 && c.DefaultTimeoutMS > c.MaxTimeoutMS) {
		return fmt.Errorf("invalid resource limits")
	}
	for _, p := range []string{c.RuntimeDir, c.StateDir, c.Shell} {
		if p != "" && !filepath.IsAbs(p) {
			return fmt.Errorf("configured path must be absolute")
		}
	}
	if err := c.ValidateGitIdentityProfiles(); err != nil {
		return err
	}
	return nil
}
