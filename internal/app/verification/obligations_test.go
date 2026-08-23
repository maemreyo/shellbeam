package verification

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

type obligationResolver struct {
	binding project.CommandBinding
	err     error
	calls   []string
}

func (r *obligationResolver) Resolve(_ context.Context, workspaceID, commandID string, _ map[string]string) (project.CommandBinding, error) {
	r.calls = append(r.calls, workspaceID+":"+commandID)
	return r.binding, r.err
}

func obligationRequirement(id string, class core.ProviderClass, command string) core.EvidenceRequirement {
	return core.EvidenceRequirement{ID: id, ProviderClass: class, ProjectCommandID: command, MinimumAuthority: core.AuthorityMechanical, RequireCurrent: true, Environment: core.EnvironmentNone, Stability: core.StabilityNoContradiction}
}
func obligationRule(id string, required bool, phases []core.Phase, paths, classes []string, ownership core.OwnershipClass, evidence ...core.EvidenceRequirement) core.Rule {
	return core.Rule{ID: id, Phases: phases, MatchPaths: paths, MatchClasses: classes, Ownership: ownership, Required: required, SufficiencyBasis: "declared:" + id, MinimumAffectedAuthority: core.AuthorityMechanical, Evidence: evidence}
}
func obligationPolicy(t *testing.T, rules []core.Rule, classifiers ...core.Classification) core.MaterializedPolicy {
	t.Helper()
	content := core.PolicyContent{SchemaVersion: 1, PolicyID: "obligation-policy", Classifiers: classifiers, Rules: rules}
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	return core.MaterializedPolicy{Snapshot: core.PolicySnapshot{RepositoryID: string(serviceWorkspace().RepositoryID), Digest: digest, Content: content}, Source: core.PolicyRepositoryAuthored, ApprovalRef: "act_obligation", ApprovalAuthority: AuthorityExplicitCaller, ApprovedAt: time.Unix(1, 0).UTC()}
}
func obligationSurface(t *testing.T, policy core.MaterializedPolicy, path string, coverage core.Coverage) core.AffectedSurface {
	t.Helper()
	base := baseSurfaceForClassification(t, path, coverage)
	got, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: base, EffectivePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func obligationBinding(t *testing.T, command string) project.CommandBinding {
	t.Helper()
	fp, err := project.ParameterFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := project.CommandBinding{SchemaVersion: project.BindingSchemaVersion, ManifestDigest: strings.Repeat("a", 64), ManifestSchemaVersion: project.ManifestSchemaV2, CommandID: command, ParameterFingerprint: fp, ResolvedArgv: []string{"go", "test", "./..."}, LogicalCWD: ".", ResolvedCWD: "/repo", Kind: "test", SourceScope: "full"}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	return b
}
func deriveObligations(t *testing.T, policy core.MaterializedPolicy, surface core.AffectedSurface, phase core.Phase, resolver ProjectCommandResolver, waivers ...core.VerificationWaiver) ObligationResult {
	t.Helper()
	got, err := NewObligationMatcher(resolver).Derive(context.Background(), ObligationRequest{WorkspaceID: string(serviceWorkspace().ID), Phase: phase, Policy: policy, Surface: surface, ActiveWaivers: waivers})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func oneObligation(t *testing.T, got ObligationResult) core.VerificationObligation {
	t.Helper()
	if len(got.Obligations) != 1 {
		t.Fatalf("obligations=%#v", got.Obligations)
	}
	return got.Obligations[0]
}

func TestObligationPhaseWideCurrentPhaseRequiredNow(t *testing.T) {
	r := obligationRule("all", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("fmt", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "docs/a.md", core.CoverageComplete), core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionRequiredNow || o.RequiredPhase != core.PhaseCheckpoint || o.EvidenceStatus != core.EvidenceNotEvaluated {
		t.Fatalf("o=%#v", o)
	}
}

func TestObligationAnyMatchingClassOrPathTriggers(t *testing.T) {
	cases := []struct {
		name, path     string
		paths, classes []string
		classifier     *core.Classification
	}{
		{"path", "cmd/a.go", []string{"cmd/**"}, nil, nil},
		{"class", "internal/auth/a.go", nil, []string{"security_sensitive"}, &core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := obligationRule("r", true, []core.Phase{core.PhaseCheckpoint}, tc.paths, tc.classes, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
			var p core.MaterializedPolicy
			if tc.classifier != nil {
				p = obligationPolicy(t, []core.Rule{r}, *tc.classifier)
			} else {
				p = obligationPolicy(t, []core.Rule{r})
			}
			o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, tc.path, core.CoverageComplete), core.PhaseCheckpoint, nil))
			if o.Disposition != core.DispositionRequiredNow {
				t.Fatalf("o=%#v", o)
			}
			if len(o.TriggerRefs) == 0 {
				t.Fatal("missing positive trigger refs")
			}
		})
	}
}

func TestObligationPhaseFoldDeferredPastAndIndependent(t *testing.T) {
	cases := []struct {
		name               string
		rulePhase, current core.Phase
		want               core.ObligationDisposition
	}{
		{"later", core.PhasePreMerge, core.PhaseCheckpoint, core.DispositionDeferred},
		{"past", core.PhaseInnerLoop, core.PhaseCheckpoint, core.DispositionNotTriggered},
		{"nightly-independent", core.PhaseNightly, core.PhaseCheckpoint, core.DispositionNotTriggered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := obligationRule("r", true, []core.Phase{tc.rulePhase}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
			p := obligationPolicy(t, []core.Rule{r})
			o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "a.go", core.CoverageComplete), tc.current, nil))
			if o.Disposition != tc.want {
				t.Fatalf("got=%s want=%s", o.Disposition, tc.want)
			}
		})
	}
}

func TestObligationNonRequiredCurrentPhaseIsOptional(t *testing.T) {
	r := obligationRule("advisory", false, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "a.go", core.CoverageComplete), core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionOptional {
		t.Fatalf("o=%#v", o)
	}
}

func TestObligationCompleteNonMatchIsNotTriggered(t *testing.T) {
	r := obligationRule("cmd", true, []core.Phase{core.PhaseCheckpoint}, []string{"cmd/**"}, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "docs/a.md", core.CoverageComplete), core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionNotTriggered {
		t.Fatalf("o=%#v", o)
	}
}

func TestUncertainAffectedSurfaceCannotNarrowMandatoryObligation(t *testing.T) {
	r := obligationRule("cmd", true, []core.Phase{core.PhaseCheckpoint}, []string{"cmd/**"}, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	surface := obligationSurface(t, p, "docs/a.md", core.CoveragePartial)
	o := oneObligation(t, deriveObligations(t, p, surface, core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionRequiredNow {
		t.Fatalf("uncertainty narrowed obligation: %#v", o)
	}
}

func TestObligationClassNonMatchNeedsStrongClassificationDomain(t *testing.T) {
	r := obligationRule("security", true, []core.Phase{core.PhaseCheckpoint}, nil, []string{"security_sensitive"}, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r}, core.Classification{ID: "other", Paths: []string{"cmd/**"}, SurfaceClass: "other"})
	complete := obligationSurface(t, p, "docs/a.md", core.CoverageComplete)
	o := oneObligation(t, deriveObligations(t, p, complete, core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionNotTriggered {
		t.Fatalf("complete class nonmatch=%#v", o)
	}
	partial := complete
	for i := range partial.Domains {
		if partial.Domains[i].Kind == core.DomainPolicyClassification {
			partial.Domains[i].Coverage = core.CoveragePartial
		}
	}
	o = oneObligation(t, deriveObligations(t, p, partial, core.PhaseCheckpoint, nil))
	if o.Disposition != core.DispositionRequiredNow {
		t.Fatalf("partial class domain narrowed=%#v", o)
	}
}

func TestWaiverDispositionPreservesEvidenceStatus(t *testing.T) {
	r := obligationRule("native", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipIntegrationOwned, obligationRequirement("native", core.ProviderProjectCommand, "native_linux"))
	p := obligationPolicy(t, []core.Rule{r})
	surface := obligationSurface(t, p, "a.go", core.CoverageComplete)
	w := core.VerificationWaiver{SchemaVersion: 1, WaiverID: "wv_native", RepositoryID: p.Snapshot.RepositoryID, PolicyDigest: p.Snapshot.Digest, RuleID: "native", Phase: core.PhaseCheckpoint, Generation: surface.SourceGeneration, Authority: AuthorityExplicitCaller, Actor: "tester", Reason: "CI only", CreatedAt: time.Unix(1, 0).UTC()}
	resolver := &obligationResolver{err: errors.New("provider unavailable")}
	o := oneObligation(t, deriveObligations(t, p, surface, core.PhaseCheckpoint, resolver, w))
	if o.Disposition != core.DispositionWaived || o.EvidenceStatus != core.EvidenceUnavailable || o.WaiverID != "wv_native" {
		t.Fatalf("o=%#v", o)
	}
}

func TestObligationProjectBindingDigestIsFrozenAndFailureNeverDropsRequirement(t *testing.T) {
	r := obligationRule("full", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("suite", core.ProviderProjectCommand, "full_suite"))
	p := obligationPolicy(t, []core.Rule{r})
	surface := obligationSurface(t, p, "a.go", core.CoverageComplete)
	binding := obligationBinding(t, "full_suite")
	resolver := &obligationResolver{binding: binding}
	o := oneObligation(t, deriveObligations(t, p, surface, core.PhaseCheckpoint, resolver))
	want, err := binding.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if len(o.EvidenceRequirements) != 1 || o.EvidenceRequirements[0].ExpectedProjectBindingDigest != want || o.EvidenceStatus != core.EvidenceNotEvaluated {
		t.Fatalf("o=%#v", o)
	}
	bad := &obligationResolver{err: errors.New("missing command")}
	got := deriveObligations(t, p, surface, core.PhaseCheckpoint, bad)
	o = oneObligation(t, got)
	if len(o.EvidenceRequirements) != 1 || o.EvidenceRequirements[0].Requirement.ProjectCommandID != "full_suite" || o.EvidenceStatus != core.EvidenceUnavailable || len(got.Diagnostics) == 0 {
		t.Fatalf("got=%#v", got)
	}
}

func TestFullSuiteExistsOnlyWhenPolicyTriggersIt(t *testing.T) {
	basePolicy := obligationPolicy(t, nil)
	base := deriveObligations(t, basePolicy, obligationSurface(t, basePolicy, "a.go", core.CoverageComplete), core.PhaseCheckpoint, nil)
	if len(base.Obligations) != 0 {
		t.Fatalf("invented obligations=%#v", base.Obligations)
	}
	r := obligationRule("full", true, []core.Phase{core.PhaseCheckpoint}, []string{"release/**"}, nil, core.OwnershipApplicationOwned, obligationRequirement("suite", core.ProviderProjectCommand, "full_suite"))
	p := obligationPolicy(t, []core.Rule{r})
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "docs/a.md", core.CoverageComplete), core.PhaseCheckpoint, &obligationResolver{binding: obligationBinding(t, "full_suite")}))
	if o.Disposition != core.DispositionNotTriggered {
		t.Fatalf("full suite auto-triggered %#v", o)
	}
}

func TestDelegatedOwnershipRulePreservesIntegrationAssumptionRequirement(t *testing.T) {
	r := obligationRule("delegated", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipDelegated, obligationRequirement("assumption", core.ProviderIntegrationTest, ""))
	p := obligationPolicy(t, []core.Rule{r})
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "a.go", core.CoverageComplete), core.PhaseCheckpoint, nil))
	if o.Ownership != core.OwnershipDelegated || len(o.EvidenceRequirements) != 1 || o.EvidenceRequirements[0].Requirement.ProviderClass != core.ProviderIntegrationTest {
		t.Fatalf("o=%#v", o)
	}
}

func TestNoPerformanceRuleMeansNoInventedLoadObligation(t *testing.T) {
	r := obligationRule("format", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("fmt", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	got := deriveObligations(t, p, obligationSurface(t, p, "a.go", core.CoverageComplete), core.PhaseCheckpoint, nil)
	for _, o := range got.Obligations {
		for _, e := range o.EvidenceRequirements {
			if e.Requirement.ProviderClass == core.ProviderResourceMeasurement {
				t.Fatalf("invented load obligation %#v", o)
			}
		}
	}
}

func TestPolicyGapRequiresMechanicalClassification(t *testing.T) {
	classifier := core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"}
	p := obligationPolicy(t, nil, classifier)
	surface := obligationSurface(t, p, "internal/auth/token.go", core.CoverageComplete)
	got := deriveObligations(t, p, surface, core.PhaseCheckpoint, nil)
	if len(got.PolicyGaps) != 1 || got.PolicyGaps[0].DeclaredClass != "security_sensitive" || got.PolicyGaps[0].Authority != core.AuthorityMechanical {
		t.Fatalf("gaps=%#v", got.PolicyGaps)
	}
	advisory := surface
	for i := range advisory.Relations {
		if advisory.Relations[i].Kind == "classified_as" {
			advisory.Relations[i].DerivationAuthority = core.AuthorityAdvisory
		}
	}
	got = deriveObligations(t, p, advisory, core.PhaseCheckpoint, nil)
	if len(got.PolicyGaps) != 0 {
		t.Fatalf("advisory gap=%#v", got.PolicyGaps)
	}
	plain := obligationPolicy(t, nil)
	got = deriveObligations(t, plain, obligationSurface(t, plain, "password.go", core.CoverageComplete), core.PhaseCheckpoint, nil)
	if len(got.PolicyGaps) != 0 {
		t.Fatalf("filename heuristic gap=%#v", got.PolicyGaps)
	}
}

func TestPolicyGapDisappearsWhenApprovedClassRuleExists(t *testing.T) {
	classifier := core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"}
	r := obligationRule("security-rule", true, []core.Phase{core.PhaseCheckpoint}, nil, []string{"security_sensitive"}, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r}, classifier)
	got := deriveObligations(t, p, obligationSurface(t, p, "internal/auth/a.go", core.CoverageComplete), core.PhaseCheckpoint, nil)
	if len(got.PolicyGaps) != 0 {
		t.Fatalf("gaps=%#v", got.PolicyGaps)
	}
}

func TestObligationOrderingStableByRuleID(t *testing.T) {
	r1 := obligationRule("z", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	r2 := r1
	r2.ID = "a"
	r2.SufficiencyBasis = "declared:a"
	p := obligationPolicy(t, []core.Rule{r1, r2})
	got := deriveObligations(t, p, obligationSurface(t, p, "a.go", core.CoverageComplete), core.PhaseCheckpoint, nil)
	ids := []string{got.Obligations[0].SourceRuleID, got.Obligations[1].SourceRuleID}
	if !reflect.DeepEqual(ids, []string{"a", "z"}) {
		t.Fatalf("order=%v", ids)
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(ids, sorted) {
		t.Fatalf("not sorted %v", ids)
	}
}

func TestNotTriggeredObligationDoesNotBecomeUnavailableFromProvider(t *testing.T) {
	r := obligationRule("full", true, []core.Phase{core.PhaseCheckpoint}, []string{"release/**"}, nil, core.OwnershipApplicationOwned, obligationRequirement("suite", core.ProviderProjectCommand, "full_suite"))
	p := obligationPolicy(t, []core.Rule{r})
	resolver := &obligationResolver{err: errors.New("provider unavailable")}
	o := oneObligation(t, deriveObligations(t, p, obligationSurface(t, p, "docs/a.md", core.CoverageComplete), core.PhaseCheckpoint, resolver))
	if o.Disposition != core.DispositionNotTriggered || o.EvidenceStatus != core.EvidenceNotEvaluated || len(resolver.calls) != 0 {
		t.Fatalf("o=%#v calls=%v", o, resolver.calls)
	}
	if len(o.EvidenceRequirements) != 1 || o.EvidenceRequirements[0].Requirement.ProjectCommandID != "full_suite" || o.EvidenceRequirements[0].ExpectedProjectBindingDigest != "" {
		t.Fatalf("requirements=%#v", o.EvidenceRequirements)
	}
}

func TestWaiverFromDifferentRepositoryCannotFoldDisposition(t *testing.T) {
	r := obligationRule("r", true, []core.Phase{core.PhaseCheckpoint}, nil, nil, core.OwnershipApplicationOwned, obligationRequirement("e", core.ProviderStaticFormatCheck, ""))
	p := obligationPolicy(t, []core.Rule{r})
	surface := obligationSurface(t, p, "a.go", core.CoverageComplete)
	w := core.VerificationWaiver{SchemaVersion: 1, WaiverID: "wv_other", RepositoryID: "repo_01K00000000000000000000001", PolicyDigest: p.Snapshot.Digest, RuleID: "r", Phase: core.PhaseCheckpoint, Generation: surface.SourceGeneration, Authority: AuthorityExplicitCaller, Actor: "tester", Reason: "other repo", CreatedAt: time.Unix(1, 0).UTC()}
	o := oneObligation(t, deriveObligations(t, p, surface, core.PhaseCheckpoint, nil, w))
	if o.Disposition != core.DispositionRequiredNow || o.WaiverID != "" {
		t.Fatalf("cross-repo waiver folded: %#v", o)
	}
}
