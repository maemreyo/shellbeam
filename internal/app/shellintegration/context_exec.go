package shellintegration

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ContextHelperArmRequest struct {
	ContextExecID  string
	SessionID      string
	Authority      delegated.EffectiveAuthority
	Facts          ProviderProcessFacts
	ExpectedShell  core.ShellIdentity
	OpaqueLaunchID string
}

type ContextHelperArm struct {
	ContextExecID      string
	SessionID          string
	AuthorityEpoch     delegated.AuthorityEpoch
	ProviderGeneration string
	Shell              core.ShellIdentity
	PaneShellPID       int
	PaneTTY            string
	OpaqueLaunchID     string
	ArmedAt            time.Time
}

func (v ContextHelperArmRequest) validate() error {
	if !validFactID(v.ContextExecID, 128) || !validFactID(v.SessionID, 128) || v.SessionID != v.Facts.SessionID {
		return fmt.Errorf("invalid context helper arm identity")
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
	if _, err := ContextShellIdentity(v.ExpectedShell); err != nil {
		return err
	}
	if !validFactID(v.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper opaque launch identity")
	}
	return nil
}

func (s *Service) ArmContextHelper(ctx context.Context, req ContextHelperArmRequest) (ContextHelperArm, error) {
	var out ContextHelperArm
	if s == nil || s.probe == nil {
		return out, contextHelperArmFailure(req, failure.ContextExecUnavailable, "shell_integration_unavailable", nil)
	}
	if err := req.validate(); err != nil {
		return out, contextHelperArmFailure(req, failure.ContextExecUnavailable, "invalid_helper_arm_request", err)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if req.Authority.Owner != delegated.OwnerAgent || req.Authority.Fenced {
		return out, contextHelperArmFailure(req, failure.ContextExecNotAgentOwned, "agent_authority_unproven", nil)
	}
	if req.Facts.PaneTTY == "" || !filepath.IsAbs(req.Facts.PaneTTY) || req.Facts.CWD == "" || !filepath.IsAbs(req.Facts.CWD) {
		return out, contextHelperArmFailure(req, failure.ContextExecBoundaryUnproven, "context_facts_unproven", nil)
	}

	observation, err := s.probe.Probe(ctx, ProbeRequest{Facts: req.Facts, Expected: &req.ExpectedShell})
	if err != nil {
		return out, contextHelperArmFailure(req, failure.ContextExecBoundaryUnproven, "shell_reprobe_failed", err)
	}
	if err := observation.Validate(); err != nil {
		return out, contextHelperArmFailure(req, failure.ContextExecBoundaryUnproven, "shell_reprobe_invalid", err)
	}
	if !observation.AdapterEligible() || observation.Identity != req.ExpectedShell {
		return out, contextHelperArmFailure(req, failure.ContextExecBoundaryUnproven, "shell_identity_changed", nil)
	}
	adapter := s.adapters[observation.Identity.Family]
	armer, ok := adapter.(ContextHelperArmer)
	if adapter == nil || !ok {
		return out, contextHelperArmFailure(req, failure.ContextExecUnavailable, "shell_adapter_unavailable", nil)
	}
	spec := ContextHelperArmSpec{Shell: observation.Identity, OpaqueLaunchID: req.OpaqueLaunchID}
	if err := spec.Validate(); err != nil {
		return out, contextHelperArmFailure(req, failure.ContextExecUnavailable, "invalid_helper_arm", err)
	}
	if err := armer.ArmContextHelper(ctx, spec); err != nil {
		return out, err
	}
	return ContextHelperArm{
		ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.Authority.Epoch,
		ProviderGeneration: req.Facts.ProviderGeneration, Shell: observation.Identity, PaneShellPID: req.Facts.PanePID,
		PaneTTY: filepath.Clean(req.Facts.PaneTTY), OpaqueLaunchID: req.OpaqueLaunchID, ArmedAt: time.Now().UTC(),
	}, nil
}

func contextHelperArmFailure(req ContextHelperArmRequest, code failure.Code, reason string, cause error) error {
	details := map[string]string{
		"context_exec_id": req.ContextExecID,
		"session_id":      req.SessionID,
		"reason":          reason,
	}
	switch code {
	case failure.ContextExecStaleGeneration, failure.ContextExecNotAgentOwned, failure.ContextExecBoundaryUnproven:
		details["authority_epoch"] = strconv.FormatUint(uint64(req.Authority.Epoch), 10)
	}
	return failure.New(code, details, cause)
}
