package mcp

import (
	"fmt"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/activity"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type VerificationInputFields struct {
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
	Reason                       string                 `json:"reason,omitempty"`
	ExpiresAt                    time.Time              `json:"expires_at,omitempty"`
	ExpiresPhase                 verificationcore.Phase `json:"expires_phase,omitempty"`
}

func validateVerificationInput(v input) error {
	if _, err := workspace.ParseWorkspaceID(v.WorkspaceID); err != nil {
		return err
	}
	switch v.Action {
	case "inspect.verification":
		if v.ActivityID != "" {
			if _, err := activity.ParseID(v.ActivityID); err != nil {
				return err
			}
		}
		return v.Phase.Validate()
	case "verification.policy.preview":
		if v.Profile != "" && v.Profile != "prototype" && v.Profile != "team" && v.Profile != "production" {
			return fmt.Errorf("invalid verification profile")
		}
		return nil
	case "verification.policy.activate":
		if verificationcore.ValidateActivationID(v.ActivationID) != nil || !validVerificationPolicyDigest(v.ProposedPolicyDigest) || (v.ExpectedPreviousPolicyDigest != "absent" && !validVerificationPolicyDigest(v.ExpectedPreviousPolicyDigest)) || !validVerificationGeneration(v.ProposalGeneration) || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationText(v.Actor, 128) {
			return fmt.Errorf("invalid verification activation")
		}
		return nil
	case "verification.waiver.set":
		if verificationcore.ValidateWaiverID(v.WaiverID) != nil || !validVerificationPolicyDigest(v.PolicyDigest) || !boundedVerificationText(v.RuleID, 128) || v.Phase.Validate() != nil || (v.Generation != "" && !validVerificationGeneration(v.Generation)) || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationText(v.Actor, 128) || !boundedVerificationText(v.Reason, 1024) || (v.ExpiresPhase != "" && v.ExpiresPhase.Validate() != nil) {
			return fmt.Errorf("invalid verification waiver")
		}
		if v.CheckpointID != "" && !validCheckpointIDInput(v.CheckpointID) {
			return fmt.Errorf("invalid verification checkpoint scope")
		}
		return nil
	case "verification.waiver.revoke":
		if verificationcore.ValidateWaiverID(v.WaiverID) != nil || v.Authority != verificationapp.AuthorityExplicitCaller || !boundedVerificationText(v.Actor, 128) {
			return fmt.Errorf("invalid verification waiver revocation")
		}
		return nil
	default:
		return fmt.Errorf("invalid verification action")
	}
}

func applyVerificationInput(request *bridge.Request, in input) {
	switch in.Action {
	case "inspect.verification":
		request.VerificationInspect = verificationapp.InspectRequest{WorkspaceID: in.WorkspaceID, ActivityID: in.ActivityID, Phase: in.Phase}
	case "verification.policy.preview":
		request.VerificationPolicyPreview = verificationapp.PreviewPolicyRequest{WorkspaceID: in.WorkspaceID, Profile: in.Profile}
	case "verification.policy.activate":
		request.VerificationActivate = verificationapp.ActivateRequest{ActivationID: in.ActivationID, WorkspaceID: in.WorkspaceID, ProposedPolicyDigest: in.ProposedPolicyDigest, ExpectedPreviousDigest: in.ExpectedPreviousPolicyDigest, ProposalGeneration: in.ProposalGeneration, Authority: in.Authority, Actor: in.Actor}
	case "verification.waiver.set":
		request.VerificationWaiverSet = verificationapp.SetWaiverRequest{WaiverID: in.WaiverID, WorkspaceID: in.WorkspaceID, PolicyDigest: in.PolicyDigest, RuleID: in.RuleID, Phase: in.Phase, Generation: in.Generation, CheckpointID: in.CheckpointID, Authority: in.Authority, Actor: in.Actor, Reason: in.Reason, ExpiresAt: in.ExpiresAt, ExpiresPhase: in.ExpiresPhase}
	case "verification.waiver.revoke":
		request.VerificationWaiverRevoke = verificationapp.RevokeWaiverRequest{WaiverID: in.WaiverID, WorkspaceID: in.WorkspaceID, Authority: in.Authority, Actor: in.Actor}
	}
}
