package shellintegration

import (
	"fmt"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type BoundaryQuality string

const (
	BoundaryQualityShellPrompt     BoundaryQuality = "shell_prompt"
	BoundaryQualityProcessBoundary BoundaryQuality = "process_boundary"
	BoundaryQualityHumanAttested   BoundaryQuality = "human_attested"
)

func (v BoundaryQuality) Validate() error {
	switch v {
	case BoundaryQualityShellPrompt, BoundaryQualityProcessBoundary, BoundaryQualityHumanAttested:
		return nil
	default:
		return fmt.Errorf("invalid shell boundary quality")
	}
}

type BoundaryProof struct {
	HandoffID      string                   `json:"handoff_id"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch"`
	Shell          ShellIdentity            `json:"shell"`
	Quality        BoundaryQuality          `json:"quality"`
	ObservedAt     time.Time                `json:"observed_at"`
}

func (v BoundaryProof) Validate() error {
	if !validOpaque(v.HandoffID, 128) {
		return fmt.Errorf("invalid boundary handoff identity")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.Shell.Validate(); err != nil {
		return err
	}
	if err := v.Quality.Validate(); err != nil {
		return err
	}
	if v.Quality == BoundaryQualityShellPrompt && v.Shell.Family == ShellUnknown {
		return fmt.Errorf("shell prompt boundary requires known shell")
	}
	if v.ObservedAt.IsZero() {
		return fmt.Errorf("boundary proof observation missing")
	}
	return nil
}

func (v BoundaryProof) CurrentFor(handoffID string, epoch delegated.AuthorityEpoch, shell ShellIdentity) bool {
	return v.Validate() == nil && shell.Validate() == nil && v.HandoffID == handoffID && v.AuthorityEpoch == epoch && v.Shell == shell
}

type PrivacyReleaseProof struct {
	HandoffID      string                   `json:"handoff_id"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch"`
	Shell          ShellIdentity            `json:"shell"`
	Boundary       string                   `json:"boundary"`
	ForwardOnly    bool                     `json:"forward_only"`
	ObservedAt     time.Time                `json:"observed_at"`
}

func (v PrivacyReleaseProof) Validate() error {
	if !validOpaque(v.HandoffID, 128) || !validOpaque(v.Boundary, 256) {
		return fmt.Errorf("invalid privacy release identity")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.Shell.Validate(); err != nil {
		return err
	}
	if v.Shell.Family == ShellUnknown {
		return fmt.Errorf("privacy release requires known shell")
	}
	if !v.ForwardOnly {
		return fmt.Errorf("privacy release must be forward only")
	}
	if v.ObservedAt.IsZero() {
		return fmt.Errorf("privacy release observation missing")
	}
	return nil
}

func (v PrivacyReleaseProof) CurrentFor(handoffID string, epoch delegated.AuthorityEpoch, shell ShellIdentity) bool {
	return v.Validate() == nil && shell.Validate() == nil && v.HandoffID == handoffID && v.AuthorityEpoch == epoch && v.Shell == shell
}
