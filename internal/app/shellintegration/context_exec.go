package shellintegration

import (
	"context"
	"fmt"
	"strconv"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ContextHelperLaunchRequest struct {
	ContextExecID  string
	HandoffID      string
	Authority      delegated.EffectiveAuthority
	Facts          ProviderProcessFacts
	ExpectedShell  core.ShellIdentity
	Boundary       core.BoundaryProof
	OpaqueLaunchID string
}

func (v ContextHelperLaunchRequest) validate() error {
	if !validFactID(v.ContextExecID, 128) || !validFactID(v.HandoffID, 128) {
		return fmt.Errorf("invalid context helper launch identity")
	}
	if err := v.Authority.Epoch.Validate(); err != nil {
		return err
	}
	if err := v.Authority.Owner.Validate(); err != nil {
		return err
	}
	if err := v.Facts.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedShell.Validate(); err != nil {
		return err
	}
	if v.ExpectedShell.Family == core.ShellUnknown {
		return fmt.Errorf("context helper launch requires exact shell")
	}
	if err := v.Boundary.Validate(); err != nil {
		return err
	}
	if !validFactID(v.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper opaque launch identity")
	}
	return nil
}

func (s *Service) LaunchContextHelper(ctx context.Context, req ContextHelperLaunchRequest) error {
	if s == nil || s.probe == nil {
		return contextHelperLaunchFailure(req, failure.ContextExecUnavailable, "shell_integration_unavailable", nil)
	}
	if err := req.validate(); err != nil {
		return contextHelperLaunchFailure(req, failure.ContextExecUnavailable, "invalid_helper_launch_request", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.Authority.Owner != delegated.OwnerAgent || req.Authority.Fenced {
		return contextHelperLaunchFailure(req, failure.ContextExecNotAgentOwned, "agent_authority_unproven", nil)
	}
	if req.Authority.Epoch != req.Boundary.AuthorityEpoch {
		return contextHelperLaunchFailure(req, failure.ContextExecStaleGeneration, "boundary_epoch_mismatch", nil)
	}
	if req.Boundary.Quality != core.BoundaryQualityShellPrompt ||
		!req.Boundary.CurrentFor(req.HandoffID, req.Authority.Epoch, req.ExpectedShell) {
		return contextHelperLaunchFailure(req, failure.ContextExecBoundaryUnproven, "prompt_boundary_unproven", nil)
	}

	observation, err := s.probe.Probe(ctx, ProbeRequest{Facts: req.Facts, Expected: &req.ExpectedShell})
	if err != nil {
		return contextHelperLaunchFailure(req, failure.ContextExecBoundaryUnproven, "shell_reprobe_failed", err)
	}
	if err := observation.Validate(); err != nil {
		return contextHelperLaunchFailure(req, failure.ContextExecBoundaryUnproven, "shell_reprobe_invalid", err)
	}
	if !observation.AdapterEligible() || observation.Identity != req.ExpectedShell {
		return contextHelperLaunchFailure(req, failure.ContextExecBoundaryUnproven, "shell_identity_changed", nil)
	}
	adapter := s.adapters[observation.Identity.Family]
	launcher, ok := adapter.(ContextHelperLauncher)
	if adapter == nil || !ok {
		return contextHelperLaunchFailure(req, failure.ContextExecUnavailable, "shell_adapter_unavailable", nil)
	}
	launch := ContextHelperLaunch{Shell: observation.Identity, OpaqueLaunchID: req.OpaqueLaunchID}
	if err := launch.Validate(); err != nil {
		return contextHelperLaunchFailure(req, failure.ContextExecUnavailable, "invalid_helper_launch", err)
	}
	return launcher.LaunchContextHelper(ctx, launch)
}

func contextHelperLaunchFailure(req ContextHelperLaunchRequest, code failure.Code, reason string, cause error) error {
	details := map[string]string{
		"context_exec_id": req.ContextExecID,
		"session_id":      req.Facts.SessionID,
		"reason":          reason,
	}
	switch code {
	case failure.ContextExecStaleGeneration, failure.ContextExecNotAgentOwned, failure.ContextExecBoundaryUnproven:
		details["authority_epoch"] = strconv.FormatUint(uint64(req.Authority.Epoch), 10)
	}
	return failure.New(code, details, cause)
}
