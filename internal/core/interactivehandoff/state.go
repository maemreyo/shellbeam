package interactivehandoff

import (
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const StateSchemaVersion = 1

type Phase string

const (
	PhaseAgentOwned      Phase = "agent_owned"
	PhaseAgentFencing    Phase = "agent_fencing"
	PhaseHumanConnecting Phase = "human_connecting"
	PhaseHumanOwned      Phase = "human_owned"
	PhaseHumanFencing    Phase = "human_fencing"
	PhaseReclaimPending  Phase = "reclaim_pending"
	PhaseAborted         Phase = "aborted"
)

func (v Phase) Validate() error {
	switch v {
	case PhaseAgentOwned, PhaseAgentFencing, PhaseHumanConnecting, PhaseHumanOwned, PhaseHumanFencing, PhaseReclaimPending, PhaseAborted:
		return nil
	default:
		return fmt.Errorf("invalid handoff phase")
	}
}

type IngressState string

const (
	IngressUnknown  IngressState = "unknown"
	IngressFenced   IngressState = "fenced"
	IngressWritable IngressState = "writable"
)

func (v IngressState) Validate() error {
	switch v {
	case IngressUnknown, IngressFenced, IngressWritable:
		return nil
	default:
		return fmt.Errorf("invalid ingress state")
	}
}

type BoundaryKind string

const (
	BoundaryNone            BoundaryKind = "none"
	BoundaryShell           BoundaryKind = "shell_boundary"
	BoundaryProcess         BoundaryKind = "process_boundary"
	BoundaryProviderOrdered BoundaryKind = "provider_ordered"
	BoundaryHumanAttested   BoundaryKind = "human_attested"
)

type TransferBoundary struct {
	Kind        BoundaryKind `json:"kind"`
	Established bool         `json:"established"`
}

func (v TransferBoundary) Validate() error {
	switch v.Kind {
	case BoundaryNone:
		if v.Established {
			return fmt.Errorf("none transfer boundary cannot be established")
		}
		return nil
	case BoundaryShell, BoundaryProcess, BoundaryProviderOrdered, BoundaryHumanAttested:
		if !v.Established {
			return fmt.Errorf("transfer boundary not established")
		}
		return nil
	default:
		return fmt.Errorf("invalid transfer boundary")
	}
}

type PrivacyState string

const (
	PrivacyStateStandard PrivacyState = "standard"
	PrivacyPrivate       PrivacyState = "private"
)

func (v PrivacyState) Validate() error {
	if v != PrivacyStateStandard && v != PrivacyPrivate {
		return fmt.Errorf("invalid privacy state")
	}
	return nil
}

type PrivacyReleaseState string

const (
	PrivacyReleaseNotRequired PrivacyReleaseState = "not_required"
	PrivacyReleasePending     PrivacyReleaseState = "pending"
	PrivacyReleaseProven      PrivacyReleaseState = "proven"
)

func (v PrivacyReleaseState) Validate() error {
	switch v {
	case PrivacyReleaseNotRequired, PrivacyReleasePending, PrivacyReleaseProven:
		return nil
	default:
		return fmt.Errorf("invalid privacy release state")
	}
}

type CaptureState string

const (
	CapturePublic  CaptureState = "public"
	CapturePrivate CaptureState = "private"
)

func (v CaptureState) Validate() error {
	if v != CapturePublic && v != CapturePrivate {
		return fmt.Errorf("invalid capture state")
	}
	return nil
}

type HumanClientRef struct {
	Ref string `json:"ref"`
}

func (v HumanClientRef) Validate() error {
	if !validOpaque(v.Ref, 256) {
		return fmt.Errorf("invalid human client ref")
	}
	return nil
}

type State struct {
	SchemaVersion      int                      `json:"schema_version"`
	HandoffID          string                   `json:"handoff_id"`
	SessionID          string                   `json:"session_id"`
	Phase              Phase                    `json:"phase"`
	AuthorityEpoch     delegated.AuthorityEpoch `json:"authority_epoch"`
	DesiredOwner       delegated.Owner          `json:"desired_owner"`
	ProviderOwner      delegated.Owner          `json:"provider_owner"`
	AgentIngress       IngressState             `json:"agent_ingress"`
	HumanIngress       IngressState             `json:"human_ingress"`
	TransferBoundary   TransferBoundary         `json:"transfer_boundary"`
	PrivacyState       PrivacyState             `json:"privacy_state"`
	PrivacyRelease     PrivacyReleaseState      `json:"privacy_release"`
	CaptureState       CaptureState             `json:"capture_state"`
	HumanClient        *HumanClientRef          `json:"human_client,omitempty"`
	ProviderGeneration string                   `json:"provider_generation"`
}

func (v State) Validate() error {
	if v.SchemaVersion != StateSchemaVersion || !validOpaque(v.HandoffID, 128) || !validOpaque(v.SessionID, 128) || !validOpaque(v.ProviderGeneration, 256) {
		return fmt.Errorf("invalid handoff state identity")
	}
	if err := v.Phase.Validate(); err != nil {
		return err
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.DesiredOwner.Validate(); err != nil {
		return err
	}
	if err := v.ProviderOwner.Validate(); err != nil {
		return err
	}
	if err := v.AgentIngress.Validate(); err != nil {
		return err
	}
	if err := v.HumanIngress.Validate(); err != nil {
		return err
	}
	if v.AgentIngress == IngressWritable && v.HumanIngress == IngressWritable {
		return fmt.Errorf("dual writable ingress")
	}
	if err := v.TransferBoundary.Validate(); err != nil {
		return err
	}
	if err := v.PrivacyState.Validate(); err != nil {
		return err
	}
	if err := v.PrivacyRelease.Validate(); err != nil {
		return err
	}
	if err := v.CaptureState.Validate(); err != nil {
		return err
	}
	if v.HumanClient != nil {
		if err := v.HumanClient.Validate(); err != nil {
			return err
		}
	}
	if v.HumanIngress == IngressWritable && v.HumanClient == nil {
		return fmt.Errorf("human writable without proven client ref")
	}
	if v.PrivacyState == PrivacyStateStandard && (v.PrivacyRelease != PrivacyReleaseNotRequired || v.CaptureState != CapturePublic) {
		return fmt.Errorf("standard privacy state mismatch")
	}
	if v.PrivacyState == PrivacyPrivate && v.PrivacyRelease == PrivacyReleasePending && v.CaptureState != CapturePrivate {
		return fmt.Errorf("pending privacy release requires private capture")
	}
	return nil
}

func (v State) ValidateH2() error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.PrivacyState != PrivacyStateStandard || v.PrivacyRelease != PrivacyReleaseNotRequired || v.CaptureState != CapturePublic {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "secret_handoff"}, nil)
	}
	return nil
}
