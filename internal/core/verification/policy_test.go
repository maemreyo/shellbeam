package verification

import (
	"reflect"
	"testing"
	"time"
)

func boolp(v bool) *bool { return &v }
func validRequirement() EvidenceRequirement {
	return EvidenceRequirement{ID: "fmt", ProviderClass: ProviderStaticFormatCheck, MinimumAuthority: AuthorityMechanical, RequireCurrent: true, Environment: EnvironmentNone, Stability: StabilityNoContradiction, Execution: ProviderExecutionSemantics{ParallelSafe: boolp(true), SharedResources: []string{"go-cache"}, ExpectedWorkloadClass: "light"}}
}
func validRule() Rule {
	return Rule{ID: "r1", Phases: []Phase{PhaseCheckpoint}, Ownership: OwnershipApplicationOwned, Required: true, SufficiencyBasis: "repository checkpoint policy", MinimumAffectedAuthority: AuthorityMechanical, Evidence: []EvidenceRequirement{validRequirement()}}
}
func validPolicy() PolicyContent {
	return PolicyContent{SchemaVersion: 1, PolicyID: "policy", Classifiers: []Classification{{ID: "c2", Paths: []string{"internal/**"}, SurfaceClass: "internal"}, {ID: "c1", Paths: []string{"cmd/**"}, SurfaceClass: "cmd"}}, Rules: []Rule{func() Rule { r := validRule(); r.ID = "r2"; return r }(), validRule()}}
}

func TestPolicyDigestCanonicalAndAuthorityIndependent(t *testing.T) {
	a := validPolicy()
	b := validPolicy()
	b.Classifiers[0], b.Classifiers[1] = b.Classifiers[1], b.Classifiers[0]
	b.Rules[0], b.Rules[1] = b.Rules[1], b.Rules[0]
	da, err := PolicyDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := PolicyDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("unordered policy sets changed digest: %s != %s", da, db)
	}
	snap := PolicySnapshot{RepositoryID: "repo_A", Digest: da, Content: a}
	materialized := MaterializedPolicy{Snapshot: snap, Source: PolicyRepositoryAuthored, ApprovalRef: "act_1", ApprovalAuthority: "explicit_caller", ApprovedAt: time.Unix(10, 0)}
	d2, err := PolicyDigest(materialized.Snapshot.Content)
	if err != nil {
		t.Fatal(err)
	}
	if d2 != da {
		t.Fatal("authority projection changed policy digest")
	}
	changed := a
	changed.PolicyID = "other"
	dc, err := PolicyDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dc == da {
		t.Fatal("semantic policy change did not change digest")
	}
}

func TestProviderExecutionSemanticsNeverChoosesUniversalConcurrency(t *testing.T) {
	p := validPolicy()
	d1, err := PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Rules[0].Evidence[0].Execution.ExpectedWorkloadClass = "heavy"
	d2, err := PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("execution semantics missing from digest")
	}
	typ := reflect.TypeOf(ProviderExecutionSemantics{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Name == "WorkerCount" || typ.Field(i).Name == "Concurrency" {
			t.Fatalf("scheduler field leaked into policy: %s", typ.Field(i).Name)
		}
	}
}

func TestPolicyValidationRejectsInvalidRulesAndClosedEnums(t *testing.T) {
	cases := map[string]func(*PolicyContent){
		"missing sufficiency":       func(p *PolicyContent) { p.Rules[0].SufficiencyBasis = "" },
		"missing phase":             func(p *PolicyContent) { p.Rules[0].Phases = nil },
		"required missing evidence": func(p *PolicyContent) { p.Rules[0].Evidence = nil },
		"ownership":                 func(p *PolicyContent) { p.Rules[0].Ownership = "mystery" },
		"risk":                      func(p *PolicyContent) { p.Rules[0].RiskClass = "mystery" },
		"environment":               func(p *PolicyContent) { p.Rules[0].Evidence[0].Environment = "mystery" },
		"workload":                  func(p *PolicyContent) { p.Rules[0].Evidence[0].Execution.ExpectedWorkloadClass = "huge" },
		"duplicate shared":          func(p *PolicyContent) { p.Rules[0].Evidence[0].Execution.SharedResources = []string{"cache", "cache"} },
		"exclusive shared": func(p *PolicyContent) {
			p.Rules[0].Evidence[0].Execution.SharedResources = []string{"cache"}
			p.Rules[0].Evidence[0].Execution.ExclusiveResourceClass = "cache"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := validPolicy()
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("expected invalid policy")
			}
		})
	}
}

func TestFlakeProtocolValidationIsClosed(t *testing.T) {
	p := validPolicy()
	req := &p.Rules[0].Evidence[0]
	req.Stability = StabilityFlakeProtocol
	req.Flake = &FlakeProtocol{Runs: 3, MinPasses: 2, MaxFailures: 1}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p = validPolicy()
	req = &p.Rules[0].Evidence[0]
	req.Stability = StabilityFlakeProtocol
	req.Flake = nil
	if err := p.Validate(); err == nil {
		t.Fatal("missing flake block accepted")
	}
	p = validPolicy()
	req = &p.Rules[0].Evidence[0]
	req.Stability = StabilityNoContradiction
	req.Flake = &FlakeProtocol{Runs: 3, MinPasses: 2, MaxFailures: 1}
	if err := p.Validate(); err == nil {
		t.Fatal("flake block on non-flake stability accepted")
	}
}

func TestWaivedAndSatisfiedBelongToDifferentClosedEnums(t *testing.T) {
	if EvidenceStatus(DispositionWaived).Validate() == nil {
		t.Fatal("waived parsed as evidence status")
	}
	if ObligationDisposition(EvidenceSatisfied).Validate() == nil {
		t.Fatal("satisfied parsed as disposition")
	}
}
