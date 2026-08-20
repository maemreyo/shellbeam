package verification

import (
	"fmt"
	"sort"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/pelletier/go-toml/v2"
)

func PreviewStarter(profile, repositoryID string, manifest *project.Manifest) (core.PolicyProposal, []string, error) {
	phases, ok := map[string][]struct {
		name  string
		phase core.Phase
	}{"prototype": {{"coding", core.PhaseInnerLoop}}, "team": {{"coding", core.PhaseInnerLoop}, {"checkpoint", core.PhaseCheckpoint}}, "production": {{"coding", core.PhaseInnerLoop}, {"checkpoint", core.PhaseCheckpoint}, {"release", core.PhaseRelease}}}[profile]
	if !ok {
		return core.PolicyProposal{}, nil, fmt.Errorf("unknown starter profile %q", profile)
	}
	content := core.PolicyContent{SchemaVersion: 1, PolicyID: "starter-" + profile + "-v1"}
	advisories := []string{}
	if manifest == nil {
		advisories = append(advisories, "project_manifest_absent")
	} else {
		for _, entry := range phases {
			vp, exists := manifest.VerificationProfiles[entry.name]
			if !exists {
				advisories = append(advisories, "verification_profile_absent:"+entry.name)
				continue
			}
			for _, id := range vp.Steps {
				cmd, exists := manifest.Commands[id]
				if !exists {
					advisories = append(advisories, "project_command_missing:"+id)
					continue
				}
				if manifest.SchemaVersion != 2 {
					advisories = append(advisories, "typed_binding_requires_manifest_v2:"+id)
					continue
				}
				if cmd.Shell != "" || len(cmd.Argv) == 0 {
					advisories = append(advisories, "typed_binding_shell_unsupported:"+id)
					continue
				}
				params := map[string]string{}
				eligible := true
				keys := make([]string, 0, len(cmd.Params))
				for k := range cmd.Params {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					def := cmd.Params[k]
					if def.Required {
						advisories = append(advisories, "parameter_declaration_required:"+id)
						eligible = false
						break
					}
					params[k] = def.Default
				}
				if !eligible {
					continue
				}
				ev := core.EvidenceRequirement{ID: "evidence_" + id, ProviderClass: core.ProviderProjectCommand, ProjectCommandID: id, Params: params, MinimumAuthority: core.AuthorityMechanical, RequireCurrent: true, Environment: core.EnvironmentNone, Stability: core.StabilityNoContradiction}
				content.Rules = append(content.Rules, core.Rule{ID: "starter_" + string(entry.phase) + "_" + id, Phases: []core.Phase{entry.phase}, Ownership: core.OwnershipApplicationOwned, Required: true, SufficiencyBasis: "repository_verification_profile:" + entry.name, MinimumAffectedAuthority: core.AuthorityMechanical, Evidence: []core.EvidenceRequirement{ev}})
			}
		}
	}
	sort.Strings(advisories)
	digest, err := core.PolicyDigest(content)
	if err != nil {
		return core.PolicyProposal{}, nil, err
	}
	return core.PolicyProposal{RepositoryID: repositoryID, Digest: digest, Origin: core.ProposalStarterProfile, ProfileOrigin: "shellbeam/" + profile + "@v1", Content: content}, advisories, nil
}

func RenderPolicyTOML(p core.PolicyProposal) ([]byte, error) {
	raw := rawPolicy{SchemaVersion: &p.Content.SchemaVersion, PolicyID: p.Content.PolicyID, ProfileOrigin: p.ProfileOrigin}
	for _, c := range p.Content.Classifiers {
		raw.Classifications = append(raw.Classifications, rawClassification{ID: c.ID, Paths: c.Paths, SurfaceClass: c.SurfaceClass})
	}
	for _, r := range p.Content.Rules {
		rr := rawRule{ID: r.ID, MatchClasses: r.MatchClasses, MatchPaths: r.MatchPaths, Ownership: string(r.Ownership), RiskClass: string(r.RiskClass), Required: r.Required, SufficiencyBasis: r.SufficiencyBasis, MinimumAffectedAuthority: string(r.MinimumAffectedAuthority)}
		for _, ph := range r.Phases {
			rr.Phases = append(rr.Phases, string(ph))
		}
		for _, e := range r.Evidence {
			re := rawEvidence{ID: e.ID, ProviderClass: string(e.ProviderClass), ProjectCommandID: e.ProjectCommandID, Params: e.Params, MinimumAuthority: string(e.MinimumAuthority), RequireCurrent: e.RequireCurrent, Environment: string(e.Environment), Stability: string(e.Stability), RequireQuiescence: e.RequireQuiescence, Execution: rawExecution{ParallelSafe: e.Execution.ParallelSafe, SharedResources: e.Execution.SharedResources, ExclusiveResourceClass: e.Execution.ExclusiveResourceClass, ExpectedWorkloadClass: e.Execution.ExpectedWorkloadClass}}
			if e.Flake != nil {
				re.Flake = &rawFlake{Runs: e.Flake.Runs, MinPasses: e.Flake.MinPasses, MaxFailures: e.Flake.MaxFailures}
			}
			rr.Evidence = append(rr.Evidence, re)
		}
		raw.Rules = append(raw.Rules, rr)
	}
	return toml.Marshal(raw)
}
