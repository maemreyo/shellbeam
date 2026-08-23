// Package receipt defines evidence required to interpret command outcomes.
package receipt

import (
	"fmt"
	"path/filepath"
	"strings"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type SpawnEvidence struct {
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
	ErrorCode string `json:"error_code,omitempty"`
}
type ResourceQuality string

const (
	ResourceExact            ResourceQuality = "exact"
	ResourcePlatformReported ResourceQuality = "platform_reported"
	ResourceSampled          ResourceQuality = "sampled"
	ResourceUnavailable      ResourceQuality = "unavailable"
)

type ResourceMetric struct {
	Quality ResourceQuality `json:"quality"`
	Value   *int64          `json:"value,omitempty"`
}

type ResourceEvidence struct {
	CPUUserMS        ResourceMetric `json:"cpu_user_ms"`
	CPUSystemMS      ResourceMetric `json:"cpu_system_ms"`
	MaxRSSBytes      ResourceMetric `json:"max_rss_bytes"`
	ReadBytes        ResourceMetric `json:"read_bytes"`
	WriteBytes       ResourceMetric `json:"write_bytes"`
	ProcessCountPeak ResourceMetric `json:"process_count_peak"`
}

func (e ResourceEvidence) Validate() error {
	for _, metric := range []ResourceMetric{e.CPUUserMS, e.CPUSystemMS, e.MaxRSSBytes, e.ReadBytes, e.WriteBytes, e.ProcessCountPeak} {
		if err := metric.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (m ResourceMetric) validate() error {
	switch m.Quality {
	case ResourceUnavailable:
		if m.Value != nil {
			return fmt.Errorf("unavailable resource metric has value")
		}
	case ResourceExact, ResourcePlatformReported, ResourceSampled:
		if m.Value == nil || *m.Value < 0 {
			return fmt.Errorf("observed resource metric lacks non-negative value")
		}
	default:
		return fmt.Errorf("invalid resource metric quality")
	}
	return nil
}

type ExitEvidence struct {
	Reaped    bool              `json:"reaped"`
	Code      *int              `json:"code,omitempty"`
	Signal    string            `json:"signal,omitempty"`
	Resources *ResourceEvidence `json:"-"`
}
type SignalEvidence struct {
	Requested string `json:"requested,omitempty"`
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
}

type ContextExecProvenance struct {
	ContextExecID       string                   `json:"context_exec_id"`
	ParentSessionID     string                   `json:"parent_session_id"`
	AuthorityEpoch      delegated.AuthorityEpoch `json:"authority_epoch"`
	RequestedExecutable string                   `json:"requested_executable"`
	ResolvedExecutable  string                   `json:"resolved_executable"`
}

func (v ContextExecProvenance) Validate() error {
	if !validContextExecReceiptRef(v.ContextExecID) || !validContextExecReceiptRef(v.ParentSessionID) || v.AuthorityEpoch < 1 {
		return fmt.Errorf("invalid context exec receipt provenance identity")
	}
	if v.RequestedExecutable == "" || strings.IndexByte(v.RequestedExecutable, 0) >= 0 || v.ResolvedExecutable == "" || !filepath.IsAbs(v.ResolvedExecutable) {
		return fmt.Errorf("invalid context exec executable provenance")
	}
	return nil
}

func validContextExecReceiptRef(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for i := range value {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

type ResourceCleanupStatus string

const ResourceCleanupIncomplete ResourceCleanupStatus = "incomplete"

type ResourceCleanup struct {
	Status ResourceCleanupStatus `json:"status"`
	Reason string                `json:"reason"`
}

func (c ResourceCleanup) Validate() error {
	if c.Status != ResourceCleanupIncomplete {
		return fmt.Errorf("invalid resource cleanup status")
	}
	switch c.Reason {
	case "final_events_unavailable", "cleanup_kill_failed", "cleanup_events_failed", "cleanup_events_invalid", "cleanup_timeout", "cleanup_remove_failed", "cleanup_unknown":
		return nil
	default:
		return fmt.Errorf("invalid resource cleanup reason")
	}
}

type HermeticCleanupStatus string

const HermeticCleanupIncomplete HermeticCleanupStatus = "incomplete"

type HermeticCleanup struct {
	Status HermeticCleanupStatus `json:"status"`
	Reason string                `json:"reason"`
}

func (c HermeticCleanup) Validate() error {
	if c.Status != HermeticCleanupIncomplete || c.Reason != "discard_failed" {
		return fmt.Errorf("invalid hermetic cleanup metadata")
	}
	return nil
}

type Receipt struct {
	SchemaVersion                 int                      `json:"schema_version"`
	OperationID                   string                   `json:"operation_id"`
	SessionID                     string                   `json:"session_id"`
	Fingerprint                   string                   `json:"fingerprint,omitempty"`
	RequestFingerprint            string                   `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string                   `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string                   `json:"observation_binding_fingerprint,omitempty"`
	DaemonIncarnation             string                   `json:"daemon_incarnation"`
	ExecutionMode                 string                   `json:"execution_mode,omitempty"`
	Executable                    string                   `json:"executable,omitempty"`
	State                         session.State            `json:"state"`
	Outcome                       session.Outcome          `json:"outcome"`
	Shell                         string                   `json:"shell,omitempty"`
	CWD                           string                   `json:"cwd,omitempty"`
	TTY                           bool                     `json:"tty"`
	TimeoutMS                     int64                    `json:"timeout_ms"`
	Persistent                    bool                     `json:"persistent,omitempty"`
	SessionName                   string                   `json:"session_name,omitempty"`
	SessionMode                   string                   `json:"session_mode,omitempty"`
	AuthorityEpoch                delegated.AuthorityEpoch `json:"authority_epoch,omitempty"`
	EvidenceAuthority             string                   `json:"evidence_authority,omitempty"`
	ContextExec                   *ContextExecProvenance   `json:"context_exec,omitempty"`
	InputAuthorityProvenance      string                   `json:"input_authority_provenance,omitempty"`
	CaptureQuality                CaptureQuality           `json:"capture_quality,omitempty"`
	CaptureReasons                []CaptureReason          `json:"capture_reasons,omitempty"`
	OutputBytes                   int64                    `json:"output_bytes"`
	OutputComplete                bool                     `json:"output_complete"`
	InputAcceptedBytes            int64                    `json:"input_accepted_bytes"`
	InputDeliveredBytes           int64                    `json:"input_delivered_bytes"`
	StdinClosed                   bool                     `json:"stdin_closed"`
	// StdinMode and TimeoutSource say who decided, not just what happened.
	// Without them a reader cannot tell a child that closed its own input from
	// one whose input was never opened, nor an explicitly requested bound from
	// the one policy supplied -- and an agent that guesses wrong either retries
	// a command that will never behave differently, or concludes ShellBeam cut
	// it off.
	StdinMode           string                    `json:"stdin_mode,omitempty"`
	TimeoutSource       string                    `json:"timeout_source,omitempty"`
	StdinModeSource     string                    `json:"stdin_mode_source,omitempty"`
	FailureReason       string                    `json:"failure_reason,omitempty"`
	ResourceCleanup     *ResourceCleanup          `json:"resource_cleanup,omitempty"`
	HermeticCleanup     *HermeticCleanup          `json:"hermetic_cleanup,omitempty"`
	HermeticBinding     *hermetic.BoundaryBinding `json:"hermetic_boundary,omitempty"`
	HermeticResult      *hermetic.BoundaryResult  `json:"hermetic_result,omitempty"`
	WorkspaceProvenance *WorkspaceProvenance      `json:"workspace_provenance,omitempty"`
	ProjectCommand      *project.CommandBinding   `json:"project_command,omitempty"`
	Evidence            *evidence.Contract        `json:"evidence,omitempty"`
	Spawn               SpawnEvidence             `json:"spawn_evidence"`
	Exit                ExitEvidence              `json:"exit_evidence"`
	Signal              SignalEvidence            `json:"signal_evidence"`
}

func (r Receipt) validateResourceCleanup() error {
	if r.ResourceCleanup == nil {
		return nil
	}
	switch r.SchemaVersion {
	case 1:
		return fmt.Errorf("derived provenance requires newer receipt schema")
	case 2, 3:
		return r.ResourceCleanup.Validate()
	case 4:
		return fmt.Errorf("resource cleanup metadata unsupported for persistent receipt")
	case 5:
		return fmt.Errorf("resource cleanup metadata unsupported for delegated receipt")
	default:
		return nil
	}
}

func (r Receipt) hasDelegatedFields() bool {
	return r.SessionMode != "" || r.AuthorityEpoch != 0 || r.EvidenceAuthority != "" || r.InputAuthorityProvenance != "" || r.CaptureQuality != "" || len(r.CaptureReasons) != 0
}

func (r Receipt) validateHermeticCleanup() error {
	if r.HermeticCleanup == nil {
		return nil
	}
	if r.SchemaVersion != 2 && r.SchemaVersion != 3 {
		return fmt.Errorf("hermetic cleanup metadata requires ephemeral v2 or v3 receipt")
	}
	if r.HermeticBinding == nil {
		return fmt.Errorf("hermetic cleanup metadata requires boundary binding")
	}
	return r.HermeticCleanup.Validate()
}

func (r Receipt) validateHermeticBoundary() error {
	if r.HermeticBinding == nil && r.HermeticResult == nil {
		return nil
	}
	if r.SchemaVersion != 2 && r.SchemaVersion != 3 {
		return fmt.Errorf("hermetic boundary metadata requires ephemeral v2 or v3 receipt")
	}
	if r.HermeticBinding == nil || r.HermeticResult == nil {
		return fmt.Errorf("incomplete hermetic boundary receipt")
	}
	return hermetic.ValidateBoundaryCompletion(*r.HermeticBinding, *r.HermeticResult)
}

func (r Receipt) Validate() error {
	if err := r.validateResourceCleanup(); err != nil {
		return err
	}
	if err := r.validateHermeticBoundary(); err != nil {
		return err
	}
	if err := r.validateHermeticCleanup(); err != nil {
		return err
	}
	if r.SchemaVersion < 5 && r.hasDelegatedFields() {
		return fmt.Errorf("delegated receipt fields require v5")
	}
	if err := r.validateSchema(); err != nil {
		return err
	}
	return r.validateCommonTruth()
}

func (r Receipt) validateSchema() error {
	switch r.SchemaVersion {
	case 1:
		if r.ProjectCommand != nil || r.Evidence != nil {
			return fmt.Errorf("derived provenance requires newer receipt schema")
		}
		return nil
	case 2:
		return r.validateV2()
	case 3:
		return r.validateV3()
	case 4:
		return r.validateV4()
	case 5:
		return r.validateV5()
	case 6:
		return r.validateV6()
	default:
		return fmt.Errorf("unsupported receipt schema")
	}
}

func (r Receipt) validateV2() error {
	if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" {
		return fmt.Errorf("v2 receipt fingerprints missing")
	}
	if r.ProjectCommand != nil {
		return fmt.Errorf("project command provenance requires v3 receipt")
	}
	if r.Evidence != nil {
		if err := r.Evidence.Validate(); err != nil {
			return fmt.Errorf("invalid evidence provenance: %w", err)
		}
	}
	return nil
}

func (r Receipt) validateV3() error {
	if r.Evidence != nil {
		return fmt.Errorf("v3 receipt cannot carry raw evidence provenance")
	}
	if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" || r.ProjectCommand == nil {
		return fmt.Errorf("v3 receipt project command provenance missing")
	}
	if err := r.ProjectCommand.Validate(); err != nil {
		return fmt.Errorf("invalid v3 project command provenance: %w", err)
	}
	return nil
}

func (r Receipt) validateV4() error {
	if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" || !r.Persistent || r.TTY {
		return fmt.Errorf("v4 persistent receipt identity missing")
	}
	if r.SessionName != "" {
		if err := persistentsession.ValidateSessionName(r.SessionName); err != nil {
			return fmt.Errorf("invalid v4 session name: %w", err)
		}
	}
	if r.ProjectCommand != nil {
		if r.Evidence != nil {
			return fmt.Errorf("v4 typed receipt cannot carry raw evidence provenance")
		}
		if err := r.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid v4 project command provenance: %w", err)
		}
	} else if r.Evidence != nil {
		if err := r.Evidence.Validate(); err != nil {
			return fmt.Errorf("invalid evidence provenance: %w", err)
		}
	}
	return nil
}

func (r Receipt) validateV5() error {
	if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" || r.SessionMode != delegated.ModeDelegatedInteractive || r.AuthorityEpoch < 1 || r.Persistent || r.TTY {
		return fmt.Errorf("v5 delegated receipt identity missing")
	}
	if r.Exit.Reaped {
		return fmt.Errorf("v5 delegated receipt cannot claim daemon reap")
	}
	if r.Evidence != nil {
		return fmt.Errorf("v5 delegated receipt cannot carry ordinary evidence provenance")
	}
	if r.ProjectCommand != nil {
		if err := r.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid v5 project command provenance: %w", err)
		}
	}
	if r.EvidenceAuthority != EvidenceAuthoritySessionLifecycleOnly {
		return fmt.Errorf("invalid v5 evidence authority")
	}
	if err := ValidateInputAuthorityProvenance(r.InputAuthorityProvenance); err != nil {
		return err
	}
	return ValidateCaptureTruth(r.CaptureQuality, r.CaptureReasons, r.OutputComplete)
}

func (r Receipt) validateV6() error {
	if r.RequestFingerprint == "" || r.ExecutionFingerprint == "" || r.ExecutionMode != "argv" || r.Executable == "" || !filepath.IsAbs(r.Executable) || r.ContextExec == nil {
		return fmt.Errorf("v6 context exec receipt identity missing")
	}
	if err := r.ContextExec.Validate(); err != nil {
		return err
	}
	if r.ContextExec.AuthorityEpoch != r.AuthorityEpoch || r.ContextExec.ResolvedExecutable != r.Executable {
		return fmt.Errorf("v6 context exec receipt provenance mismatch")
	}
	if r.SessionMode != "" || r.Persistent || r.TTY || r.ProjectCommand != nil || r.Evidence != nil || r.InputAuthorityProvenance != "" || r.CaptureQuality != "" || len(r.CaptureReasons) != 0 {
		return fmt.Errorf("v6 context exec receipt claims unsupported authority")
	}
	if r.EvidenceAuthority != EvidenceAuthorityContextExecChildOwnedV1 {
		return fmt.Errorf("invalid v6 context exec evidence authority")
	}
	if !r.Spawn.Attempted || !r.Spawn.Succeeded || !r.Exit.Reaped {
		return fmt.Errorf("v6 context exec receipt lacks literal child truth")
	}
	return nil
}

func (r Receipt) validateCommonTruth() error {
	if r.InputDeliveredBytes > r.InputAcceptedBytes {
		return fmt.Errorf("delivered input exceeds accepted")
	}
	if r.Exit.Resources != nil {
		if err := r.Exit.Resources.Validate(); err != nil {
			return fmt.Errorf("invalid exit resource evidence: %w", err)
		}
	}
	if r.State == session.Completed && r.Outcome == session.Success {
		reapRequired := r.SchemaVersion != 5
		outputCompleteRequired := r.SchemaVersion != 5
		if !r.Spawn.Attempted || !r.Spawn.Succeeded || (reapRequired && !r.Exit.Reaped) || r.Exit.Code == nil || *r.Exit.Code != 0 || (outputCompleteRequired && !r.OutputComplete) || r.InputAcceptedBytes != r.InputDeliveredBytes {
			return fmt.Errorf("success lacks complete evidence")
		}
	}
	if r.State == session.Abandoned && r.Outcome != session.Ambiguous {
		return fmt.Errorf("abandoned must be ambiguous")
	}
	if r.State.Terminal() && r.Outcome == session.NoOutcome {
		return fmt.Errorf("terminal outcome missing")
	}
	return r.validateWorkspaceProvenance()
}

func (r Receipt) validateWorkspaceProvenance() error {
	if r.WorkspaceProvenance == nil {
		return nil
	}
	if r.SchemaVersion < 2 {
		return fmt.Errorf("workspace provenance requires v2 receipt")
	}
	return r.WorkspaceProvenance.Validate()
}
