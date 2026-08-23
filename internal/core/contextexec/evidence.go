package contextexec

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const EvidenceAuthorityContextExecChildOwnedV1 = "context_exec_child_owned_v1"

type OutputAttribution string

const OutputAttributionHelperOwnedChildPipes OutputAttribution = "helper_owned_child_pipes"

type EvidenceQuality string

const (
	EvidenceQualityUnproven   EvidenceQuality = "unproven"
	EvidenceQualityIncomplete EvidenceQuality = "incomplete"
	EvidenceQualityComplete   EvidenceQuality = "complete"
	EvidenceQualityAmbiguous  EvidenceQuality = "ambiguous"
)

func (v EvidenceQuality) Validate() error {
	switch v {
	case EvidenceQualityUnproven, EvidenceQualityIncomplete, EvidenceQualityComplete, EvidenceQualityAmbiguous:
		return nil
	default:
		return fmt.Errorf("invalid context exec evidence quality")
	}
}

type ExecutableIdentity struct {
	Requested    string `json:"requested"`
	ResolvedPath string `json:"resolved_path"`
}

func (v ExecutableIdentity) Validate() error {
	if v.Requested == "" || len(v.Requested) > MaxPathBytes || strings.IndexByte(v.Requested, 0) >= 0 {
		return fmt.Errorf("invalid requested executable identity")
	}
	if v.ResolvedPath == "" || len(v.ResolvedPath) > MaxPathBytes || !filepath.IsAbs(v.ResolvedPath) {
		return fmt.Errorf("invalid resolved executable identity")
	}
	return nil
}

type OutputEvidence struct {
	StdoutBytes    int64             `json:"stdout_bytes"`
	StderrBytes    int64             `json:"stderr_bytes"`
	OutputComplete bool              `json:"output_complete"`
	Truncated      bool              `json:"truncated,omitempty"`
	Attribution    OutputAttribution `json:"attribution"`
}

func (v OutputEvidence) Validate() error {
	if v.StdoutBytes < 0 || v.StderrBytes < 0 {
		return fmt.Errorf("invalid context exec output bytes")
	}
	if v.Attribution != OutputAttributionHelperOwnedChildPipes {
		return fmt.Errorf("invalid context exec output attribution")
	}
	if v.Truncated && v.OutputComplete {
		return fmt.Errorf("truncated context exec output cannot be complete")
	}
	return nil
}

type Result struct {
	SchemaVersion      int                    `json:"schema_version"`
	ContextExecID      string                 `json:"context_exec_id"`
	RequestFingerprint string                 `json:"request_fingerprint"`
	Lifecycle          Lifecycle              `json:"lifecycle"`
	Context            ContextBinding         `json:"context"`
	Helper             *HelperBinding         `json:"helper,omitempty"`
	Executable         ExecutableIdentity     `json:"executable"`
	Spawn              receipt.SpawnEvidence  `json:"spawn"`
	Exit               receipt.ExitEvidence   `json:"exit"`
	Signal             receipt.SignalEvidence `json:"signal"`
	TimedOut           bool                   `json:"timed_out"`
	Output             OutputEvidence         `json:"output"`
	EvidenceQuality    EvidenceQuality        `json:"evidence_quality"`
	EvidenceAuthority  string                 `json:"evidence_authority,omitempty"`
	FailureCode        string                 `json:"failure_code,omitempty"`
}

func (r Result) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("invalid context exec result schema")
	}
	if !validOpaque(r.ContextExecID, MaxContextExecIDBytes) || !validSHA256(r.RequestFingerprint) {
		return fmt.Errorf("invalid context exec result identity")
	}
	if err := r.Lifecycle.Validate(); err != nil {
		return err
	}
	if err := r.Context.Validate(); err != nil {
		return err
	}
	if err := r.validateHelperBinding(); err != nil {
		return err
	}
	if err := r.EvidenceQuality.Validate(); err != nil {
		return err
	}
	if r.EvidenceAuthority != "" && r.EvidenceAuthority != EvidenceAuthorityContextExecChildOwnedV1 {
		return fmt.Errorf("invalid context exec evidence authority")
	}
	switch r.Lifecycle {
	case LifecycleChildTerminal:
		return r.validateChildTerminal(false)
	case LifecycleCanonicalized:
		if r.FailureCode != "" && !r.Spawn.Succeeded {
			return r.validateCanonicalNoChildFailure()
		}
		return r.validateChildTerminal(true)
	case LifecycleHelperLost, LifecycleAmbiguous:
		return r.validateAmbiguousResult()
	default:
		if r.EvidenceAuthority != "" {
			return fmt.Errorf("nonterminal context exec result cannot claim evidence authority")
		}
		return nil
	}
}

func (r Result) validateHelperBinding() error {
	if r.Helper == nil {
		return nil
	}
	if err := r.Helper.Validate(); err != nil {
		return err
	}
	if r.Helper.RequestFingerprint != r.RequestFingerprint {
		return fmt.Errorf("context helper request fingerprint mismatch")
	}
	return nil
}

func (r Result) validateChildTerminal(canonical bool) error {
	label := "child terminal"
	if canonical {
		label = "canonical"
	}
	if r.Helper == nil {
		return fmt.Errorf("%s context exec result lacks helper binding", label)
	}
	if err := r.Executable.Validate(); err != nil {
		return err
	}
	if !r.Spawn.Attempted || !r.Spawn.Succeeded || !r.Exit.Reaped {
		return fmt.Errorf("%s context exec result lacks literal terminal evidence", label)
	}
	if err := r.Output.Validate(); err != nil {
		return err
	}
	if canonical {
		if r.EvidenceAuthority != EvidenceAuthorityContextExecChildOwnedV1 {
			return fmt.Errorf("canonical context exec result lacks child-owned evidence authority")
		}
	} else if r.EvidenceAuthority != "" {
		return fmt.Errorf("child terminal context exec result cannot claim evidence authority")
	}
	return r.validateOutputQuality(label)
}

func (r Result) validateOutputQuality(label string) error {
	if r.Output.OutputComplete {
		if r.Output.Truncated || r.EvidenceQuality != EvidenceQualityComplete {
			return fmt.Errorf("complete %s output has inconsistent evidence quality", label)
		}
		return nil
	}
	if r.EvidenceQuality != EvidenceQualityIncomplete {
		return fmt.Errorf("incomplete %s output has inconsistent evidence quality", label)
	}
	return nil
}

func (r Result) validateCanonicalNoChildFailure() error {
	if !validOpaque(r.FailureCode, MaxOpaqueRefBytes) || r.Helper == nil {
		return fmt.Errorf("canonical no-child context exec failure lacks stable identity")
	}
	if r.EvidenceAuthority != "" || r.EvidenceQuality != EvidenceQualityUnproven {
		return fmt.Errorf("canonical no-child context exec failure cannot claim mechanical evidence")
	}
	if r.Exit.Reaped || r.Exit.Code != nil || r.Exit.Signal != "" || r.Signal.Attempted || r.Signal.Succeeded || r.Signal.Requested != "" || r.TimedOut {
		return fmt.Errorf("canonical no-child context exec failure carries child terminal evidence")
	}
	if r.Output != (OutputEvidence{}) {
		return fmt.Errorf("canonical no-child context exec failure carries output evidence")
	}
	if r.Spawn.Succeeded {
		return fmt.Errorf("canonical no-child context exec failure claims successful spawn")
	}
	if !r.Spawn.Attempted {
		if r.Spawn.ErrorCode != "" || r.Executable != (ExecutableIdentity{}) {
			return fmt.Errorf("prepare failure carries spawn or executable evidence")
		}
		return nil
	}
	if r.Spawn.ErrorCode != r.FailureCode {
		return fmt.Errorf("failed spawn error code mismatch")
	}
	return r.Executable.Validate()
}

func (r Result) validateAmbiguousResult() error {
	if r.EvidenceQuality != EvidenceQualityAmbiguous || r.EvidenceAuthority != "" {
		return fmt.Errorf("ambiguous context exec result cannot claim evidence authority")
	}
	if r.Executable.Requested != "" || r.Executable.ResolvedPath != "" {
		if err := r.Executable.Validate(); err != nil {
			return err
		}
	}
	if r.Output.Attribution != "" || r.Output.StdoutBytes != 0 || r.Output.StderrBytes != 0 || r.Output.OutputComplete || r.Output.Truncated {
		if err := r.Output.Validate(); err != nil {
			return err
		}
	}
	return nil
}
