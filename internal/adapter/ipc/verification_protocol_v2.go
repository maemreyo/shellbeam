package ipc

import (
	"context"
	"fmt"
	"strings"
	"time"

	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type VerificationRequestV2Fields struct {
	Phase                        verificationcore.Phase `json:"phase,omitempty"`
	Profile                      string                 `json:"profile,omitempty"`
	ActivationID                 string                 `json:"activation_id,omitempty"`
	ProposedPolicyDigest         string                 `json:"proposed_policy_digest,omitempty"`
	ExpectedPreviousPolicyDigest string                 `json:"expected_previous_policy_digest,omitempty"`
	ProposalGeneration           string                 `json:"proposal_generation,omitempty"`
	Authority                    string                 `json:"authority,omitempty"`
	Actor                        string                 `json:"actor,omitempty"`
	WaiverID                     string                 `json:"waiver_id,omitempty"`
	PolicyDigest                 string                 `json:"policy_digest,omitempty"`
	RuleID                       string                 `json:"rule_id,omitempty"`
	Generation                   string                 `json:"generation,omitempty"`
	ExpiresAt                    *time.Time             `json:"expires_at,omitempty"`
	ExpiresPhase                 verificationcore.Phase `json:"expires_phase,omitempty"`
}

type VerificationResponseV2Fields struct {
	Verification              *verificationapp.Inspection             `json:"verification,omitempty"`
	VerificationPolicyPreview *verificationapp.PolicyPreview          `json:"verification_policy_preview,omitempty"`
	VerificationActivation    *verificationcore.ActivationWriteResult `json:"verification_activation,omitempty"`
	VerificationWaiver        *verificationcore.WaiverWriteResult     `json:"verification_waiver,omitempty"`
	VerificationRevocation    *verificationcore.RevocationWriteResult `json:"verification_revocation,omitempty"`
}

type VerificationActions interface {
	InspectVerification(context.Context, verificationapp.InspectRequest) (verificationapp.Inspection, error)
	PreviewVerificationPolicy(context.Context, verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error)
	ActivateVerificationPolicy(context.Context, verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error)
	SetVerificationWaiver(context.Context, verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error)
	RevokeVerificationWaiver(context.Context, verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error)
}

func validateVerificationRequestV2(v RequestV2) error {
	if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
	}
	switch v.Action {
	case "inspect.verification":
		if v.ActivityID != "" {
			if _, err := activity.ParseID(v.ActivityID); err != nil {
				return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
			}
		}
		if err := v.Phase.Validate(); err != nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "phase"}, err)
		}
	case "verification.policy.preview":
		if v.Profile != "" && v.Profile != "prototype" && v.Profile != "team" && v.Profile != "production" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "profile"}, fmt.Errorf("invalid verification profile"))
		}
	case "verification.policy.activate":
		if verificationcore.ValidateActivationID(v.ActivationID) != nil || !validPolicyDigestV2(v.ProposedPolicyDigest) || (v.ExpectedPreviousPolicyDigest != "absent" && !validPolicyDigestV2(v.ExpectedPreviousPolicyDigest)) || !validGenerationV2(v.ProposalGeneration) || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationTextV2(v.Actor, 128) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "verification_activation"}, fmt.Errorf("invalid activation request"))
		}
	case "verification.waiver.set":
		if verificationcore.ValidateWaiverID(v.WaiverID) != nil || !validPolicyDigestV2(v.PolicyDigest) || !boundedVerificationTextV2(v.RuleID, 128) || v.Phase.Validate() != nil || (v.Generation != "" && !validGenerationV2(v.Generation)) || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationTextV2(v.Actor, 128) || !boundedVerificationTextV2(v.Reason, 1024) || (v.ExpiresPhase != "" && v.ExpiresPhase.Validate() != nil) || (v.CheckpointID != "" && !validVerificationCheckpointIDV2(v.CheckpointID)) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "verification_waiver"}, fmt.Errorf("invalid waiver request"))
		}
	case "verification.waiver.revoke":
		if verificationcore.ValidateWaiverID(v.WaiverID) != nil || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationTextV2(v.Actor, 128) {
			return failure.New(failure.InvalidInput, map[string]string{"field": "verification_waiver_revoke"}, fmt.Errorf("invalid waiver revocation request"))
		}
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
	return nil
}

func validPolicyDigestV2(v string) bool {
	return len(v) == 68 && v[:4] == "pol_" && validLowerHexV2(v[4:])
}
func validGenerationV2(v string) bool {
	return len(v) == 68 && v[:4] == "gen_" && validLowerHexV2(v[4:])
}
func validLowerHexV2(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func boundedVerificationTextV2(v string, max int) bool { return v != "" && len(v) <= max }

func validVerificationCheckpointIDV2(value string) bool {
	if len(value) != 30 || !strings.HasPrefix(value, "chk_") {
		return false
	}
	for _, r := range value[4:] {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			return false
		}
	}
	return true
}

func (s *Server) verificationV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(VerificationActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	switch req.Action {
	case "inspect.verification":
		result, err := actions.InspectVerification(ctx, verificationapp.InspectRequest{WorkspaceID: req.WorkspaceID, ActivityID: req.ActivityID, Phase: req.Phase})
		if err == nil {
			resp.Verification = &result
		}
		return err
	case "verification.policy.preview":
		result, err := actions.PreviewVerificationPolicy(ctx, verificationapp.PreviewPolicyRequest{WorkspaceID: req.WorkspaceID, Profile: req.Profile})
		if err == nil {
			resp.VerificationPolicyPreview = &result
		}
		return err
	case "verification.policy.activate":
		result, err := actions.ActivateVerificationPolicy(ctx, verificationapp.ActivateRequest{ActivationID: req.ActivationID, WorkspaceID: req.WorkspaceID, ProposedPolicyDigest: req.ProposedPolicyDigest, ExpectedPreviousDigest: req.ExpectedPreviousPolicyDigest, ProposalGeneration: req.ProposalGeneration, Authority: req.Authority, Actor: req.Actor})
		if err == nil {
			resp.VerificationActivation = &result
		}
		return err
	case "verification.waiver.set":
		waiver := verificationapp.SetWaiverRequest{WaiverID: req.WaiverID, WorkspaceID: req.WorkspaceID, PolicyDigest: req.PolicyDigest, RuleID: req.RuleID, Phase: req.Phase, Generation: req.Generation, CheckpointID: req.CheckpointID, Authority: req.Authority, Actor: req.Actor, Reason: req.Reason, ExpiresPhase: req.ExpiresPhase}
		if req.ExpiresAt != nil {
			waiver.ExpiresAt = *req.ExpiresAt
		}
		result, err := actions.SetVerificationWaiver(ctx, waiver)
		if err == nil {
			resp.VerificationWaiver = &result
		}
		return err
	case "verification.waiver.revoke":
		result, err := actions.RevokeVerificationWaiver(ctx, verificationapp.RevokeWaiverRequest{WaiverID: req.WaiverID, WorkspaceID: req.WorkspaceID, Authority: req.Authority, Actor: req.Actor})
		if err == nil {
			resp.VerificationRevocation = &result
		}
		return err
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, nil)
	}
}

func bridgeVerificationActionV2(action string) bool {
	switch action {
	case "inspect.verification", "verification.policy.preview", "verification.policy.activate", "verification.waiver.set", "verification.waiver.revoke":
		return true
	default:
		return false
	}
}
