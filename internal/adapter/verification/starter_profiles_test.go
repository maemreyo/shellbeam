package verification

import (
	"context"
	"testing"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func manifestV2ForStarter() *project.Manifest {
	def := "./..."
	return &project.Manifest{SchemaVersion: 2, Commands: map[string]project.Command{
		"fixed":     {Argv: []string{"go", "test", "./..."}},
		"defaulted": {Argv: []string{"go", "test", "{pkg}"}, Params: map[string]project.ParameterDefinition{"pkg": {Kind: project.ParameterString, Required: false, Default: def}}},
		"required":  {Argv: []string{"go", "test", "{pkg}"}, Params: map[string]project.ParameterDefinition{"pkg": {Kind: project.ParameterString, Required: true}}},
		"shell":     {Shell: "go test ./..."},
	}, VerificationProfiles: map[string]project.VerificationProfile{
		"coding":     {Steps: []string{"fixed", "required", "shell"}},
		"checkpoint": {Steps: []string{"defaulted"}},
		"release":    {Steps: []string{"fixed"}},
	}}
}

func TestStarterProfilesUseOnlyExplicitNamedProfilesAndEligibleBindings(t *testing.T) {
	p, adv, err := PreviewStarter("team", "repo_01K00000000000000000000000", manifestV2ForStarter())
	if err != nil {
		t.Fatal(err)
	}
	if p.Origin != core.ProposalStarterProfile || p.ProfileOrigin != "shellbeam/team@v1" {
		t.Fatalf("proposal=%#v", p)
	}
	if len(p.Content.Rules) != 2 {
		t.Fatalf("rules=%#v advisories=%v", p.Content.Rules, adv)
	}
	phases := map[string]core.Phase{}
	for _, r := range p.Content.Rules {
		if len(r.Phases) != 1 {
			t.Fatalf("phases=%v", r.Phases)
		}
		phases[r.Evidence[0].ProjectCommandID] = r.Phases[0]
	}
	if phases["fixed"] != core.PhaseInnerLoop || phases["defaulted"] != core.PhaseCheckpoint {
		t.Fatalf("phases=%v", phases)
	}
	if len(adv) != 2 || adv[0] != "parameter_declaration_required:required" || adv[1] != "typed_binding_shell_unsupported:shell" {
		t.Fatalf("advisories=%v", adv)
	}
}

func TestStarterProfilesNeverInventNFRTargets(t *testing.T) {
	p, _, err := PreviewStarter("production", "repo_01K00000000000000000000000", manifestV2ForStarter())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Content.Rules {
		if r.RiskClass != core.RiskClass("") {
			t.Fatalf("invented risk=%q", r.RiskClass)
		}
		for _, e := range r.Evidence {
			if e.Execution.ExpectedWorkloadClass != "" || e.Execution.ExclusiveResourceClass != "" || e.Execution.ParallelSafe != nil {
				t.Fatalf("invented execution semantics=%#v", e.Execution)
			}
		}
	}
}

func TestStarterManifestV1AndNoProfilesAreAdvisoryOnly(t *testing.T) {
	m := manifestV2ForStarter()
	m.SchemaVersion = 1
	p, adv, err := PreviewStarter("prototype", "repo_01K00000000000000000000000", m)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Content.Rules) != 0 || len(adv) == 0 {
		t.Fatalf("rules=%v adv=%v", p.Content.Rules, adv)
	}
	p, adv, err = PreviewStarter("prototype", "repo_01K00000000000000000000000", &project.Manifest{SchemaVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Content.Rules) != 0 || len(adv) == 0 {
		t.Fatalf("empty rules=%v adv=%v", p.Content.Rules, adv)
	}
}

func TestStarterRenderedRoundTripBecomesRepositoryAuthoredWithProfileProvenance(t *testing.T) {
	proposal, _, err := PreviewStarter("team", "repo_01K00000000000000000000000", manifestV2ForStarter())
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderPolicyTOML(proposal)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writePolicy(t, root, string(rendered))
	loaded := NewPolicyLoader().Load(context.Background(), testWorkspace(root))
	if loaded.State != PolicyLoadValid || loaded.Proposal == nil {
		t.Fatalf("loaded=%#v", loaded)
	}
	if loaded.Proposal.Origin != core.ProposalRepositoryAuthored || loaded.Proposal.ProfileOrigin != proposal.ProfileOrigin || loaded.Proposal.Digest != proposal.Digest {
		t.Fatalf("roundtrip proposal=%#v loaded=%#v", proposal, loaded.Proposal)
	}
}
