package verification

import (
	"fmt"
	"sort"
	"time"
)

type ProposalOrigin string

const (
	ProposalRepositoryAuthored ProposalOrigin = "repository_authored"
	ProposalStarterProfile     ProposalOrigin = "starter_profile"
	ProposalGenerated          ProposalOrigin = "generated_proposal"
)

type PolicySource string

const PolicyRepositoryAuthored PolicySource = "repository_authored"

type PolicyContent struct {
	SchemaVersion int              `json:"schema_version"`
	PolicyID      string           `json:"policy_id"`
	Classifiers   []Classification `json:"classifications,omitempty"`
	Rules         []Rule           `json:"rules"`
}
type PolicyProposal struct {
	RepositoryID  string         `json:"repository_id"`
	Digest        string         `json:"policy_digest"`
	Origin        ProposalOrigin `json:"proposal_origin"`
	ProfileOrigin string         `json:"profile_origin,omitempty"`
	Content       PolicyContent  `json:"content"`
}
type PolicySnapshot struct {
	RepositoryID string        `json:"repository_id"`
	Digest       string        `json:"policy_digest"`
	Content      PolicyContent `json:"content"`
}
type MaterializedPolicy struct {
	Snapshot          PolicySnapshot `json:"snapshot"`
	Source            PolicySource   `json:"source"`
	ProfileOrigin     string         `json:"profile_origin,omitempty"`
	ApprovalRef       string         `json:"approval_ref"`
	ApprovalAuthority string         `json:"approval_authority"`
	ApprovedAt        time.Time      `json:"approved_at"`
}
type PolicySummary struct {
	PolicyID          string       `json:"policy_id"`
	Digest            string       `json:"policy_digest"`
	Source            PolicySource `json:"source"`
	ProfileOrigin     string       `json:"profile_origin,omitempty"`
	ApprovalRef       string       `json:"approval_ref,omitempty"`
	ApprovalAuthority string       `json:"approval_authority,omitempty"`
	ApprovedAt        time.Time    `json:"approved_at,omitempty"`
}
type PolicyProposalSummary struct {
	PolicyID      string         `json:"policy_id"`
	Digest        string         `json:"policy_digest"`
	Origin        ProposalOrigin `json:"proposal_origin"`
	ProfileOrigin string         `json:"profile_origin,omitempty"`
}
type Classification struct {
	ID           string   `json:"id"`
	Paths        []string `json:"paths"`
	SurfaceClass string   `json:"surface_class"`
}

type OwnershipClass string

const (
	OwnershipApplicationOwned OwnershipClass = "application_owned"
	OwnershipIntegrationOwned OwnershipClass = "integration_owned"
	OwnershipDelegated        OwnershipClass = "delegated"
)

type RiskClass string

const (
	RiskScaleDriven   RiskClass = "scale_driven"
	RiskRiskDriven    RiskClass = "risk_driven"
	RiskContextDriven RiskClass = "context_driven"
	RiskDelegated     RiskClass = "delegated"
)

type Rule struct {
	ID                       string                `json:"id"`
	Phases                   []Phase               `json:"phases"`
	MatchClasses             []string              `json:"match_classes,omitempty"`
	MatchPaths               []string              `json:"match_paths,omitempty"`
	Ownership                OwnershipClass        `json:"ownership"`
	RiskClass                RiskClass             `json:"risk_class,omitempty"`
	Required                 bool                  `json:"required"`
	SufficiencyBasis         string                `json:"sufficiency_basis"`
	MinimumAffectedAuthority DerivationAuthority   `json:"minimum_affected_authority"`
	Evidence                 []EvidenceRequirement `json:"evidence"`
}
type ProviderClass string

const (
	ProviderProjectCommand             ProviderClass = "project_command"
	ProviderStaticFormatCheck          ProviderClass = "static_format_check"
	ProviderFocusedBehaviorTest        ProviderClass = "focused_behavior_test"
	ProviderIntegrationTest            ProviderClass = "integration_test"
	ProviderTypecheckCompiler          ProviderClass = "typecheck_compiler"
	ProviderSchemaCompatibility        ProviderClass = "schema_compatibility"
	ProviderBrowserUserJourney         ProviderClass = "browser_user_journey"
	ProviderNativePlatformVerification ProviderClass = "native_platform_verification"
	ProviderArtifactDigest             ProviderClass = "artifact_digest"
	ProviderResourceMeasurement        ProviderClass = "resource_measurement"
	ProviderReleaseCheck               ProviderClass = "release_check"
)

type EnvironmentRequirement string

const (
	EnvironmentNone                 EnvironmentRequirement = "none"
	EnvironmentSameCurrent          EnvironmentRequirement = "same_current"
	EnvironmentSameCurrentToolchain EnvironmentRequirement = "same_current_toolchain"
)

type StabilityRequirement string

const (
	StabilitySingleCurrentPass StabilityRequirement = "single_current_pass"
	StabilityNoContradiction   StabilityRequirement = "no_contradiction"
	StabilityFlakeProtocol     StabilityRequirement = "flake_protocol"
)

type FlakeProtocol struct {
	Runs        int `json:"runs"`
	MinPasses   int `json:"min_passes"`
	MaxFailures int `json:"max_failures"`
}
type ProviderExecutionSemantics struct {
	ParallelSafe           *bool    `json:"parallel_safe,omitempty"`
	SharedResources        []string `json:"shared_resources,omitempty"`
	ExclusiveResourceClass string   `json:"exclusive_resource_class,omitempty"`
	ExpectedWorkloadClass  string   `json:"expected_workload_class,omitempty"`
}
type EvidenceRequirement struct {
	ID                string                     `json:"id"`
	ProviderClass     ProviderClass              `json:"provider_class"`
	ProjectCommandID  string                     `json:"project_command_id,omitempty"`
	Params            map[string]string          `json:"params,omitempty"`
	MinimumAuthority  DerivationAuthority        `json:"minimum_authority"`
	RequireCurrent    bool                       `json:"require_current"`
	Environment       EnvironmentRequirement     `json:"environment"`
	Stability         StabilityRequirement       `json:"stability"`
	Flake             *FlakeProtocol             `json:"flake,omitempty"`
	RequireQuiescence bool                       `json:"require_quiescence,omitempty"`
	Execution         ProviderExecutionSemantics `json:"execution,omitempty"`
}
type BoundEvidenceRequirement struct {
	Requirement                  EvidenceRequirement `json:"requirement"`
	ExpectedProjectBindingDigest string              `json:"expected_project_binding_digest,omitempty"`
}

func (p PolicyContent) Validate() error {
	if p.SchemaVersion != 1 || !boundedToken(p.PolicyID, 128) || len(p.Rules) > 128 || len(p.Classifiers) > 128 {
		return fmt.Errorf("invalid policy header")
	}
	ids := map[string]struct{}{}
	for i := range p.Classifiers {
		c := p.Classifiers[i]
		if !boundedToken(c.ID, 128) || !boundedToken(c.SurfaceClass, 128) || len(c.Paths) == 0 || len(c.Paths) > 128 {
			return fmt.Errorf("invalid classification %d", i)
		}
		if _, ok := ids["c:"+c.ID]; ok {
			return fmt.Errorf("duplicate classification id")
		}
		ids["c:"+c.ID] = struct{}{}
		if err := validateStrings(c.Paths, 128, 1024); err != nil {
			return err
		}
	}
	for i := range p.Rules {
		r := p.Rules[i]
		if err := r.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", i, err)
		}
		if _, ok := ids["r:"+r.ID]; ok {
			return fmt.Errorf("duplicate rule id")
		}
		ids["r:"+r.ID] = struct{}{}
	}
	return nil
}
func (r Rule) Validate() error {
	if !boundedToken(r.ID, 128) || len(r.Phases) == 0 || len(r.Phases) > 6 {
		return fmt.Errorf("invalid rule id/phases")
	}
	seen := map[Phase]bool{}
	for _, p := range r.Phases {
		if p.Validate() != nil || seen[p] {
			return fmt.Errorf("invalid/duplicate phase")
		}
		seen[p] = true
	}
	switch r.Ownership {
	case OwnershipApplicationOwned, OwnershipIntegrationOwned, OwnershipDelegated:
	default:
		return fmt.Errorf("invalid ownership")
	}
	if r.RiskClass != "" {
		switch r.RiskClass {
		case RiskScaleDriven, RiskRiskDriven, RiskContextDriven, RiskDelegated:
		default:
			return fmt.Errorf("invalid risk class")
		}
	}
	if r.MinimumAffectedAuthority.Validate() != nil {
		return fmt.Errorf("invalid affected authority")
	}
	if r.Required && (!boundedToken(r.SufficiencyBasis, 1024) || len(r.Evidence) == 0) {
		return fmt.Errorf("required rule missing sufficiency/evidence")
	}
	if len(r.Evidence) > 32 {
		return fmt.Errorf("too many evidence requirements")
	}
	if err := validateStrings(r.MatchClasses, 128, 128); err != nil {
		return err
	}
	if err := validateStrings(r.MatchPaths, 128, 1024); err != nil {
		return err
	}
	eids := map[string]bool{}
	for i := range r.Evidence {
		if err := r.Evidence[i].Validate(); err != nil {
			return fmt.Errorf("evidence %d: %w", i, err)
		}
		if eids[r.Evidence[i].ID] {
			return fmt.Errorf("duplicate evidence id")
		}
		eids[r.Evidence[i].ID] = true
	}
	return nil
}
func (e EvidenceRequirement) Validate() error {
	if !boundedToken(e.ID, 128) || e.ProviderClass.Validate() != nil || e.MinimumAuthority.Validate() != nil || e.Environment.Validate() != nil || e.Stability.Validate() != nil {
		return fmt.Errorf("invalid evidence requirement")
	}
	if e.ProjectCommandID != "" && !boundedToken(e.ProjectCommandID, 128) {
		return fmt.Errorf("invalid project command id")
	}
	if len(e.Params) > 32 {
		return fmt.Errorf("too many params")
	}
	for k, v := range e.Params {
		if !boundedToken(k, 64) || !boundedToken(v, 1024) {
			return fmt.Errorf("invalid param")
		}
	}
	if e.Stability == StabilityFlakeProtocol {
		if e.Flake == nil || e.Flake.Runs < 1 || e.Flake.Runs > 32 || e.Flake.MinPasses < 1 || e.Flake.MinPasses > e.Flake.Runs || e.Flake.MaxFailures < 0 || e.Flake.MaxFailures > e.Flake.Runs || e.Flake.MinPasses+e.Flake.MaxFailures < e.Flake.Runs {
			return fmt.Errorf("invalid flake protocol")
		}
	} else if e.Flake != nil {
		return fmt.Errorf("flake block without flake stability")
	}
	return e.Execution.Validate()
}
func (e ProviderExecutionSemantics) Validate() error {
	if len(e.SharedResources) > 32 {
		return fmt.Errorf("too many shared resources")
	}
	seen := map[string]bool{}
	for _, r := range e.SharedResources {
		if !boundedToken(r, 128) || seen[r] {
			return fmt.Errorf("invalid duplicate shared resource")
		}
		seen[r] = true
	}
	if e.ExclusiveResourceClass != "" {
		if !boundedToken(e.ExclusiveResourceClass, 128) || seen[e.ExclusiveResourceClass] {
			return fmt.Errorf("invalid exclusive resource class")
		}
	}
	switch e.ExpectedWorkloadClass {
	case "", "light", "moderate", "heavy", "extreme":
	default:
		return fmt.Errorf("invalid workload class")
	}
	return nil
}
func (p ProviderClass) Validate() error {
	switch p {
	case ProviderProjectCommand, ProviderStaticFormatCheck, ProviderFocusedBehaviorTest, ProviderIntegrationTest, ProviderTypecheckCompiler, ProviderSchemaCompatibility, ProviderBrowserUserJourney, ProviderNativePlatformVerification, ProviderArtifactDigest, ProviderResourceMeasurement, ProviderReleaseCheck:
		return nil
	}
	return fmt.Errorf("invalid provider class %q", p)
}
func (e EnvironmentRequirement) Validate() error {
	switch e {
	case EnvironmentNone, EnvironmentSameCurrent, EnvironmentSameCurrentToolchain:
		return nil
	}
	return fmt.Errorf("invalid environment requirement %q", e)
}
func (s StabilityRequirement) Validate() error {
	switch s {
	case StabilitySingleCurrentPass, StabilityNoContradiction, StabilityFlakeProtocol:
		return nil
	}
	return fmt.Errorf("invalid stability %q", s)
}

func canonicalPolicy(p PolicyContent) PolicyContent {
	out := p
	out.Classifiers = append([]Classification(nil), p.Classifiers...)
	sort.Slice(out.Classifiers, func(i, j int) bool { return out.Classifiers[i].ID < out.Classifiers[j].ID })
	out.Rules = append([]Rule(nil), p.Rules...)
	sort.Slice(out.Rules, func(i, j int) bool { return out.Rules[i].ID < out.Rules[j].ID })
	for i := range out.Rules {
		out.Rules[i].Evidence = append([]EvidenceRequirement(nil), out.Rules[i].Evidence...)
		sort.Slice(out.Rules[i].Evidence, func(a, b int) bool { return out.Rules[i].Evidence[a].ID < out.Rules[i].Evidence[b].ID })
		for j := range out.Rules[i].Evidence {
			ex := &out.Rules[i].Evidence[j].Execution
			ex.SharedResources = append([]string(nil), ex.SharedResources...)
			sort.Strings(ex.SharedResources)
		}
	}
	return out
}
