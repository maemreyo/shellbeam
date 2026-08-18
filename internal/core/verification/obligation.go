package verification

import "fmt"

type ObligationDisposition string

const (
	DispositionRequiredNow  ObligationDisposition = "required_now"
	DispositionDeferred     ObligationDisposition = "deferred"
	DispositionOptional     ObligationDisposition = "optional"
	DispositionNotTriggered ObligationDisposition = "not_triggered"
	DispositionWaived       ObligationDisposition = "waived"
)

type EvidenceStatus string

const (
	EvidenceNotEvaluated EvidenceStatus = "not_evaluated"
	EvidenceSatisfied    EvidenceStatus = "satisfied"
	EvidenceFailed       EvidenceStatus = "failed"
	EvidenceInsufficient EvidenceStatus = "insufficient"
	EvidenceInconsistent EvidenceStatus = "inconsistent"
	EvidenceUnknown      EvidenceStatus = "unknown"
	EvidenceUnavailable  EvidenceStatus = "unavailable"
)

type GateStatus string

const (
	GateClear         GateStatus = "clear"
	GateBlocked       GateStatus = "blocked"
	GateIndeterminate GateStatus = "indeterminate"
)

type Phase string

const (
	PhaseInnerLoop  Phase = "inner_loop"
	PhaseCheckpoint Phase = "checkpoint"
	PhasePreMerge   Phase = "pre_merge"
	PhaseRelease    Phase = "release"
	PhaseNightly    Phase = "nightly"
	PhasePeriodic   Phase = "periodic"
)

type VerificationObligation struct {
	SchemaVersion            int                        `json:"schema_version"`
	ObligationID             string                     `json:"obligation_id"`
	PolicyDigest             string                     `json:"policy_digest"`
	SourceRuleID             string                     `json:"source_rule_id"`
	TriggerRefs              []string                   `json:"trigger_refs"`
	AffectedScopeRefs        []string                   `json:"affected_scope_refs"`
	Ownership                OwnershipClass             `json:"ownership"`
	RiskClass                RiskClass                  `json:"risk_class,omitempty"`
	RequiredPhase            Phase                      `json:"required_phase"`
	SufficiencyBasis         string                     `json:"sufficiency_basis"`
	MinimumAffectedAuthority DerivationAuthority        `json:"minimum_affected_authority"`
	EvidenceRequirements     []BoundEvidenceRequirement `json:"evidence_requirements"`
	AppliesToGeneration      string                     `json:"applies_to_generation"`
	Disposition              ObligationDisposition      `json:"disposition"`
	EvidenceStatus           EvidenceStatus             `json:"evidence_status"`
	EvidenceRefs             []string                   `json:"evidence_refs,omitempty"`
	WaiverID                 string                     `json:"waiver_id,omitempty"`
}

type PolicyGap struct {
	GapID                string              `json:"gap_id"`
	SurfaceRef           string              `json:"surface_ref"`
	DeclaredClass        string              `json:"declared_class"`
	ClassificationSource string              `json:"classification_source"`
	MissingPolicyClass   string              `json:"missing_policy_class"`
	Authority            DerivationAuthority `json:"authority"`
	ProvenanceRefs       []string            `json:"provenance_refs"`
}

func (d ObligationDisposition) Validate() error {
	switch d {
	case DispositionRequiredNow, DispositionDeferred, DispositionOptional, DispositionNotTriggered, DispositionWaived:
		return nil
	}
	return fmt.Errorf("invalid obligation disposition %q", d)
}

func (s EvidenceStatus) Validate() error {
	switch s {
	case EvidenceNotEvaluated, EvidenceSatisfied, EvidenceFailed, EvidenceInsufficient, EvidenceInconsistent, EvidenceUnknown, EvidenceUnavailable:
		return nil
	}
	return fmt.Errorf("invalid evidence status %q", s)
}

func (s GateStatus) Validate() error {
	switch s {
	case GateClear, GateBlocked, GateIndeterminate:
		return nil
	}
	return fmt.Errorf("invalid gate status %q", s)
}

func (p Phase) Validate() error {
	switch p {
	case PhaseInnerLoop, PhaseCheckpoint, PhasePreMerge, PhaseRelease, PhaseNightly, PhasePeriodic:
		return nil
	}
	return fmt.Errorf("invalid phase %q", p)
}

func (o VerificationObligation) Validate() error {
	if o.SchemaVersion != 1 || !isDerivedID(o.ObligationID, "obl_") || !isDerivedID(o.PolicyDigest, "pol_") || !boundedToken(o.SourceRuleID, 128) {
		return fmt.Errorf("invalid obligation identity")
	}
	if !validGeneration(o.AppliesToGeneration) || o.RequiredPhase.Validate() != nil || o.MinimumAffectedAuthority.Validate() != nil || o.Disposition.Validate() != nil || o.EvidenceStatus.Validate() != nil {
		return fmt.Errorf("invalid obligation semantics")
	}
	if !validOwnership(o.Ownership) || !validRiskClass(o.RiskClass) || !boundedToken(o.SufficiencyBasis, 1024) {
		return fmt.Errorf("invalid obligation policy metadata")
	}
	if len(o.TriggerRefs) == 0 || len(o.AffectedScopeRefs) == 0 {
		return fmt.Errorf("obligation requires trigger/scope refs")
	}
	if err := validateRefs(o.TriggerRefs, 128, 2048); err != nil {
		return err
	}
	if err := validateRefs(o.AffectedScopeRefs, 256, 2048); err != nil {
		return err
	}
	if err := validateRefs(o.EvidenceRefs, 256, 2048); err != nil {
		return err
	}
	if len(o.EvidenceRequirements) > 32 {
		return fmt.Errorf("too many bound evidence requirements")
	}
	for i := range o.EvidenceRequirements {
		if err := o.EvidenceRequirements[i].Validate(); err != nil {
			return fmt.Errorf("bound evidence %d: %w", i, err)
		}
	}
	if o.Disposition == DispositionWaived {
		if ValidateWaiverID(o.WaiverID) != nil {
			return fmt.Errorf("waived obligation requires valid waiver id")
		}
	} else if o.WaiverID != "" {
		return fmt.Errorf("waiver id on non-waived obligation")
	}
	return nil
}

func (b BoundEvidenceRequirement) Validate() error {
	if err := b.Requirement.Validate(); err != nil {
		return err
	}
	if b.ExpectedProjectBindingDigest != "" && !validDigestHex64(b.ExpectedProjectBindingDigest) {
		return fmt.Errorf("invalid project binding digest")
	}
	if b.Requirement.ProjectCommandID == "" && b.ExpectedProjectBindingDigest != "" {
		return fmt.Errorf("binding digest without project command")
	}
	return nil
}

func (g PolicyGap) Validate() error {
	if !isDerivedID(g.GapID, "gap_") || !boundedToken(g.SurfaceRef, 2048) || !boundedToken(g.DeclaredClass, 128) || !boundedToken(g.ClassificationSource, 128) || !boundedToken(g.MissingPolicyClass, 128) {
		return fmt.Errorf("invalid policy gap identity")
	}
	if !MeetsMinimumAuthority(g.Authority, AuthorityMechanical) {
		return fmt.Errorf("policy gap lacks mechanical classification authority")
	}
	if len(g.ProvenanceRefs) == 0 {
		return fmt.Errorf("policy gap requires provenance")
	}
	return validateRefs(g.ProvenanceRefs, 128, 2048)
}

func validOwnership(v OwnershipClass) bool {
	switch v {
	case OwnershipApplicationOwned, OwnershipIntegrationOwned, OwnershipDelegated:
		return true
	}
	return false
}

func validRiskClass(v RiskClass) bool {
	switch v {
	case "", RiskScaleDriven, RiskRiskDriven, RiskContextDriven, RiskDelegated:
		return true
	}
	return false
}

func validDigestHex64(v string) bool {
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
