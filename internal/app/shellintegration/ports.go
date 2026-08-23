package shellintegration

import (
	"context"
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ProbeRequest struct {
	Facts    ProviderProcessFacts
	Expected *core.ShellIdentity
}

func (v ProbeRequest) Validate() error {
	if err := v.Facts.Validate(); err != nil {
		return err
	}
	if v.Expected != nil {
		if err := v.Expected.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type ShellProbe interface {
	Probe(context.Context, ProbeRequest) (ShellIdentityObservation, error)
}

type WatchRequest struct {
	HandoffID      string
	AuthorityEpoch delegated.AuthorityEpoch
	Shell          core.ShellIdentity
	Requirement    core.Requirement
}

func (v WatchRequest) Validate() error {
	if !validFactID(v.HandoffID, 128) {
		return fmt.Errorf("invalid watcher handoff identity")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.Shell.Validate(); err != nil {
		return err
	}
	if v.Shell.Family == core.ShellUnknown {
		return fmt.Errorf("watcher requires exact shell family")
	}
	return v.Requirement.Validate()
}

type WatchEvent struct {
	Result   core.RequirementResult
	Boundary core.BoundaryProof
}

func (v WatchEvent) Validate(req WatchRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := v.Result.Validate(); err != nil {
		return err
	}
	if v.Result.Requirement != req.Requirement || v.Result.Quality != core.RequirementQualityExactShellAdapter {
		return fmt.Errorf("watcher result authority mismatch")
	}
	if err := v.Boundary.Validate(); err != nil {
		return err
	}
	if !v.Boundary.CurrentFor(req.HandoffID, req.AuthorityEpoch, req.Shell) {
		return fmt.Errorf("watcher boundary authority mismatch")
	}
	return nil
}

type RequirementWatcher interface {
	Wait(context.Context) (WatchEvent, error)
	Close() error
}

type Adapter interface {
	Family() core.ShellFamily
	Install(context.Context, WatchRequest) (RequirementWatcher, error)
}

// ContextHelperArmSpec is the complete shell-visible context-exec arm surface.
// Target command argv, environment values, prompt proofs, and helper claim authority are intentionally absent.
type ContextHelperArmSpec struct {
	Shell          core.ShellIdentity
	OpaqueLaunchID string
}

func (v ContextHelperArmSpec) Validate() error {
	if err := v.Shell.Validate(); err != nil {
		return err
	}
	if v.Shell.Family == core.ShellUnknown {
		return fmt.Errorf("context helper arm requires exact shell")
	}
	if !validFactID(v.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper arm identity")
	}
	return nil
}

type ContextHelperArmer interface {
	Family() core.ShellFamily
	ArmContextHelper(context.Context, ContextHelperArmSpec) error
}
