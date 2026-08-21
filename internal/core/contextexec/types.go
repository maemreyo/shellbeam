// Package contextexec defines the closed high-assurance delegated context-exec contracts.
package contextexec

import (
	"fmt"
	"path/filepath"
	"strings"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

const (
	SchemaVersion = 1

	MaxContextExecIDBytes       = 128
	MaxSessionIDBytes           = 128
	MaxArgCount                 = 256
	MaxArgBytes                 = 64 << 10
	MaxOpaqueRefBytes           = 128
	MaxIdentityBytes            = 512
	MaxPathBytes                = 4096
	MaxTimeoutMS          int64 = 24 * 60 * 60 * 1000
	MaxOutputBytes        int64 = 64 << 20
)

type Request struct {
	ContextExecID  string                   `json:"context_exec_id"`
	SessionID      string                   `json:"session_id"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch"`
	Argv           []string                 `json:"argv"`
	TimeoutMS      int64                    `json:"timeout_ms"`
	MaxOutputBytes int64                    `json:"max_output_bytes"`
}

func (r Request) Clone() Request {
	out := r
	out.Argv = append([]string(nil), r.Argv...)
	return out
}

func (r Request) Validate() error {
	if !validOpaque(r.ContextExecID, MaxContextExecIDBytes) {
		return fmt.Errorf("invalid context exec identity")
	}
	if !validOpaque(r.SessionID, MaxSessionIDBytes) {
		return fmt.Errorf("invalid context exec session identity")
	}
	if err := r.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if len(r.Argv) == 0 || len(r.Argv) > MaxArgCount {
		return fmt.Errorf("invalid context exec argv count")
	}
	total := 0
	for _, arg := range r.Argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return fmt.Errorf("context exec argv contains nul")
		}
		total += len(arg)
		if total > MaxArgBytes {
			return fmt.Errorf("context exec argv exceeds byte bound")
		}
	}
	if r.Argv[0] == "" {
		return fmt.Errorf("context exec executable is empty")
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > MaxTimeoutMS {
		return fmt.Errorf("invalid context exec timeout")
	}
	if r.MaxOutputBytes < 1 || r.MaxOutputBytes > MaxOutputBytes {
		return fmt.Errorf("invalid context exec output bound")
	}
	return nil
}

type ContextBinding struct {
	SessionID       string                   `json:"session_id"`
	AuthorityEpoch  delegated.AuthorityEpoch `json:"authority_epoch"`
	ShellIdentity   string                   `json:"shell_identity"`
	BoundaryQuality string                   `json:"boundary_quality"`
	CWDObserved     string                   `json:"cwd_observed"`
	PrivacyState    string                   `json:"privacy_state"`
}

func (b ContextBinding) Validate() error {
	if !validOpaque(b.SessionID, MaxSessionIDBytes) {
		return fmt.Errorf("invalid context binding session identity")
	}
	if err := b.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if !validOpaque(b.ShellIdentity, MaxIdentityBytes) {
		return fmt.Errorf("invalid context shell identity")
	}
	if b.BoundaryQuality != "shell_prompt" && b.BoundaryQuality != "process_boundary" {
		return fmt.Errorf("invalid context boundary quality")
	}
	if b.CWDObserved == "" || len(b.CWDObserved) > MaxPathBytes || !filepath.IsAbs(b.CWDObserved) {
		return fmt.Errorf("invalid observed context cwd")
	}
	if b.PrivacyState != "standard" {
		return fmt.Errorf("context exec requires public privacy state")
	}
	return nil
}

type HelperBinding struct {
	OpaqueLaunchID     string `json:"opaque_launch_id"`
	Generation         string `json:"generation"`
	RequestFingerprint string `json:"request_fingerprint"`
	ExecutablePath     string `json:"executable_path"`
}

func (b HelperBinding) Validate() error {
	if !validOpaque(b.OpaqueLaunchID, MaxOpaqueRefBytes) || !validOpaque(b.Generation, MaxOpaqueRefBytes) {
		return fmt.Errorf("invalid context helper binding identity")
	}
	if !validSHA256(b.RequestFingerprint) {
		return fmt.Errorf("invalid context helper request fingerprint")
	}
	if b.ExecutablePath == "" || len(b.ExecutablePath) > MaxPathBytes || !filepath.IsAbs(b.ExecutablePath) {
		return fmt.Errorf("invalid context helper executable path")
	}
	return nil
}

func validOpaque(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
