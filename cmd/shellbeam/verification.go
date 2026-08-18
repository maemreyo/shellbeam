package main

import (
	"context"
	"fmt"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	verificationadapter "github.com/maemreyo/shellbeam/internal/adapter/verification"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

type daemonVerificationCoordinator interface {
	InspectVerification(context.Context, verificationapp.InspectRequest) (verificationapp.Inspection, error)
	PreviewVerificationPolicy(context.Context, verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error)
	ActivateVerificationPolicy(context.Context, verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error)
	SetVerificationWaiver(context.Context, verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error)
	RevokeVerificationWaiver(context.Context, verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error)
}

type daemonVerificationRuntime struct {
	inspection *verificationapp.InspectionService
	policy     *verificationapp.PolicyService
}

func composeVerificationRuntime(
	store *storeadapter.Repository,
	workspaces *workspaceapp.Service,
	observer *workspaceapp.Observer,
	deltas *workspaceapp.DeltaSampler,
	activities *activityapp.Service,
	projects *projectapp.Service,
	binder *projectapp.Binder,
) *daemonVerificationRuntime {
	loader := verificationadapter.NewPolicyLoader()
	commands := verificationadapter.NewProjectCommandSource(binder)
	relations := verificationadapter.NewGoRelationProvider(verificationadapter.DefaultGoRelationLimits())
	affected := verificationapp.NewAffectedService(workspaces, deltas, activities, observer, relations)
	policy := verificationapp.NewPolicyService(store, loader, store, store, observer, projects, commands)
	obligations := verificationapp.NewObligationMatcher(commands)
	inspection := verificationapp.NewInspectionService(workspaces, loader, store, affected, obligations, policy, projects, verificationadapter.NewStarterSource())
	return &daemonVerificationRuntime{inspection: inspection, policy: policy}
}

func (r *daemonVerificationRuntime) InspectVerification(ctx context.Context, req verificationapp.InspectRequest) (verificationapp.Inspection, error) {
	if r == nil || r.inspection == nil {
		return verificationapp.Inspection{}, fmt.Errorf("verification inspection unavailable")
	}
	return r.inspection.Inspect(ctx, req)
}
func (r *daemonVerificationRuntime) PreviewVerificationPolicy(ctx context.Context, req verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error) {
	if r == nil || r.inspection == nil {
		return verificationapp.PolicyPreview{}, fmt.Errorf("verification policy preview unavailable")
	}
	return r.inspection.PreviewPolicy(ctx, req)
}
func (r *daemonVerificationRuntime) ActivateVerificationPolicy(ctx context.Context, req verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error) {
	if r == nil || r.policy == nil {
		return verificationcore.ActivationWriteResult{}, fmt.Errorf("verification activation unavailable")
	}
	return r.policy.Activate(ctx, req)
}
func (r *daemonVerificationRuntime) SetVerificationWaiver(ctx context.Context, req verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error) {
	if r == nil || r.policy == nil {
		return verificationcore.WaiverWriteResult{}, fmt.Errorf("verification waiver unavailable")
	}
	return r.policy.SetWaiver(ctx, req)
}
func (r *daemonVerificationRuntime) RevokeVerificationWaiver(ctx context.Context, req verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error) {
	if r == nil || r.policy == nil {
		return verificationcore.RevocationWriteResult{}, fmt.Errorf("verification waiver revocation unavailable")
	}
	return r.policy.RevokeWaiver(ctx, req)
}

func (a *daemonActions) InspectVerification(ctx context.Context, req verificationapp.InspectRequest) (verificationapp.Inspection, error) {
	if a == nil || a.verification == nil {
		return verificationapp.Inspection{}, fmt.Errorf("verification inspection unavailable")
	}
	return a.verification.InspectVerification(ctx, req)
}
func (a *daemonActions) PreviewVerificationPolicy(ctx context.Context, req verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error) {
	if a == nil || a.verification == nil {
		return verificationapp.PolicyPreview{}, fmt.Errorf("verification policy preview unavailable")
	}
	return a.verification.PreviewVerificationPolicy(ctx, req)
}
func (a *daemonActions) ActivateVerificationPolicy(ctx context.Context, req verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error) {
	if a == nil || a.verification == nil {
		return verificationcore.ActivationWriteResult{}, fmt.Errorf("verification activation unavailable")
	}
	return a.verification.ActivateVerificationPolicy(ctx, req)
}
func (a *daemonActions) SetVerificationWaiver(ctx context.Context, req verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error) {
	if a == nil || a.verification == nil {
		return verificationcore.WaiverWriteResult{}, fmt.Errorf("verification waiver unavailable")
	}
	return a.verification.SetVerificationWaiver(ctx, req)
}
func (a *daemonActions) RevokeVerificationWaiver(ctx context.Context, req verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error) {
	if a == nil || a.verification == nil {
		return verificationcore.RevocationWriteResult{}, fmt.Errorf("verification waiver revocation unavailable")
	}
	return a.verification.RevokeVerificationWaiver(ctx, req)
}

func verificationSemanticsSupport() capability.VerificationSemanticsSupport {
	return capability.VerificationSemanticsSupport{SchemaVersions: []int{1}, PolicySchemaVersions: []int{1}, MaxDomains: 16, MaxRelations: 512, MaxObligations: 256, MaxPolicyGaps: 128, MaxPolicyRules: 128, MaxClassifications: 128, MaxEvidenceRequirementsPerRule: 32}
}
