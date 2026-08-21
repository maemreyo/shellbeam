package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

const (
	ContextExecStateSchemaVersion       = 1
	ContextExecReservationSchemaVersion = 6
)

type ContextExecBinding struct {
	ContextExecID      string                   `json:"context_exec_id"`
	ParentSessionID    SessionID                `json:"parent_session_id"`
	AuthorityEpoch     delegated.AuthorityEpoch `json:"authority_epoch"`
	RequestFingerprint string                   `json:"request_fingerprint"`
}

func (b ContextExecBinding) Validate() error {
	if !validContextExecID(b.ContextExecID) {
		return fmt.Errorf("invalid context exec binding identity")
	}
	if _, err := ParseSessionID(string(b.ParentSessionID)); err != nil {
		return err
	}
	if err := b.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if !validContextExecDigest(b.RequestFingerprint) {
		return fmt.Errorf("invalid context exec request fingerprint")
	}
	return nil
}

func (b ContextExecBinding) Digest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Version            int                      `json:"version"`
		Kind               string                   `json:"kind"`
		ContextExecID      string                   `json:"context_exec_id"`
		ParentSessionID    SessionID                `json:"parent_session_id"`
		AuthorityEpoch     delegated.AuthorityEpoch `json:"authority_epoch"`
		RequestFingerprint string                   `json:"request_fingerprint"`
	}{1, "context_exec_binding", b.ContextExecID, b.ParentSessionID, b.AuthorityEpoch, b.RequestFingerprint})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (b ContextExecBinding) ExecutionFingerprint(cwd, actualExecutable string) (string, error) {
	digest, err := b.Digest()
	if err != nil {
		return "", err
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("context exec cwd must be absolute")
	}
	if actualExecutable == "" || !filepath.IsAbs(actualExecutable) {
		return "", fmt.Errorf("context exec executable must be absolute")
	}
	raw, err := json.Marshal(struct {
		Version       int    `json:"version"`
		Kind          string `json:"kind"`
		BindingDigest string `json:"binding_digest"`
		CWD           string `json:"cwd"`
		Executable    string `json:"executable"`
	}{1, "context_exec_execution", digest, cwd, actualExecutable})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type ContextExecState struct {
	SchemaVersion      int                        `json:"schema_version"`
	Request            contextexec.Request        `json:"request"`
	RequestFingerprint string                     `json:"request_fingerprint"`
	Context            contextexec.ContextBinding `json:"context"`
	Lifecycle          contextexec.Lifecycle      `json:"lifecycle"`
	Helper             *contextexec.HelperBinding `json:"helper,omitempty"`
	ChildOperationID   ID                         `json:"child_operation_id,omitempty"`
	ChildSessionID     SessionID                  `json:"child_session_id,omitempty"`
	Result             *contextexec.Result        `json:"result,omitempty"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}

func (s ContextExecState) Validate() error {
	if s.SchemaVersion != ContextExecStateSchemaVersion {
		return fmt.Errorf("invalid context exec state schema")
	}
	if err := s.Request.Validate(); err != nil {
		return err
	}
	fingerprint, err := s.Request.Fingerprint()
	if err != nil || fingerprint != s.RequestFingerprint {
		return fmt.Errorf("context exec request fingerprint mismatch")
	}
	if err := s.Context.Validate(); err != nil {
		return err
	}
	if s.Context.SessionID != s.Request.SessionID || s.Context.AuthorityEpoch != s.Request.AuthorityEpoch {
		return fmt.Errorf("context exec context authority mismatch")
	}
	if err := s.Lifecycle.Validate(); err != nil {
		return err
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return fmt.Errorf("invalid context exec timestamps")
	}
	if s.Helper != nil {
		if err := s.Helper.Validate(); err != nil {
			return err
		}
		if s.Helper.RequestFingerprint != s.RequestFingerprint {
			return fmt.Errorf("context exec helper request mismatch")
		}
	}
	if (s.ChildOperationID == "") != (s.ChildSessionID == "") {
		return fmt.Errorf("incomplete context exec child identity")
	}
	if s.ChildOperationID != "" {
		if _, err := ParseID(string(s.ChildOperationID)); err != nil {
			return err
		}
		if _, err := ParseSessionID(string(s.ChildSessionID)); err != nil {
			return err
		}
	}
	if s.Result != nil {
		if err := s.Result.Validate(); err != nil {
			return err
		}
		if s.Result.ContextExecID != s.Request.ContextExecID || s.Result.RequestFingerprint != s.RequestFingerprint || !reflect.DeepEqual(s.Result.Context, s.Context) {
			return fmt.Errorf("context exec result identity mismatch")
		}
	}
	switch s.Lifecycle {
	case contextexec.LifecycleReserved:
		if s.Helper != nil || s.ChildOperationID != "" || s.Result != nil {
			return fmt.Errorf("reserved context exec contains runtime binding")
		}
	case contextexec.LifecycleHelperRequested, contextexec.LifecycleHelperAuthenticated:
		if s.Helper == nil || s.ChildOperationID != "" || s.Result != nil {
			return fmt.Errorf("helper context exec state is incomplete")
		}
	case contextexec.LifecycleChildSpawned:
		if s.Helper == nil || s.ChildOperationID == "" || s.Result != nil {
			return fmt.Errorf("spawned context exec state is incomplete")
		}
	case contextexec.LifecycleChildTerminal:
		if s.Helper == nil || s.ChildOperationID == "" || s.Result == nil || s.Result.Lifecycle != contextexec.LifecycleChildTerminal {
			return fmt.Errorf("terminal context exec state is incomplete")
		}
	case contextexec.LifecycleCanonicalized:
		if s.Helper == nil || s.ChildOperationID == "" || s.Result == nil || s.Result.Lifecycle != contextexec.LifecycleCanonicalized {
			return fmt.Errorf("canonical context exec state is incomplete")
		}
	case contextexec.LifecycleHelperLost, contextexec.LifecycleAmbiguous:
		if s.Result != nil && s.Result.Lifecycle != s.Lifecycle {
			return fmt.Errorf("context exec failure lifecycle mismatch")
		}
	}
	return nil
}

func (s ContextExecState) Clone() ContextExecState {
	out := s
	out.Request = s.Request.Clone()
	if s.Helper != nil {
		helper := *s.Helper
		out.Helper = &helper
	}
	if s.Result != nil {
		result := *s.Result
		if s.Result.Helper != nil {
			helper := *s.Result.Helper
			result.Helper = &helper
		}
		if s.Result.Exit.Code != nil {
			code := *s.Result.Exit.Code
			result.Exit.Code = &code
		}
		out.Result = &result
	}
	return out
}

type ContextExecTransition struct {
	Lifecycle        contextexec.Lifecycle      `json:"lifecycle"`
	Helper           *contextexec.HelperBinding `json:"helper,omitempty"`
	ChildOperationID ID                         `json:"child_operation_id,omitempty"`
	ChildSessionID   SessionID                  `json:"child_session_id,omitempty"`
	Result           *contextexec.Result        `json:"result,omitempty"`
}

func validContextExecID(value string) bool {
	if value == "" || len(value) > contextexec.MaxContextExecIDBytes {
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

func validContextExecDigest(value string) bool {
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
