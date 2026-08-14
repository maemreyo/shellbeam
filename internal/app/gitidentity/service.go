package gitidentity

import (
	"context"
	"fmt"
	coreidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
)

type Result struct {
	WorkspaceID  string                  `json:"workspace_id"`
	RepositoryID string                  `json:"repository_id"`
	Effect       string                  `json:"effect"`
	Deep         bool                    `json:"deep"`
	Resolution   coreidentity.Resolution `json:"resolution"`
	Snapshot     *coreidentity.Snapshot  `json:"snapshot,omitempty"`
	Findings     []coreidentity.Finding  `json:"findings"`
}

type Service struct {
	workspaces WorkspaceLookup
	probe      Probe
	profiles   Profiles
}

func New(workspaces WorkspaceLookup, probe Probe, profiles Profiles) *Service {
	return &Service{workspaces: workspaces, probe: probe, profiles: profiles}
}

func (s *Service) Preflight(ctx context.Context, target, effect string, deep bool) (Result, error) {
	switch effect {
	case "push", "pr", "tag", "release", "publish", "verify":
	default:
		return Result{}, fmt.Errorf("invalid identity preflight effect")
	}
	ws, err := s.workspaces.Inspect(ctx, target)
	if err != nil {
		return Result{}, err
	}
	obs, remote, err := s.probe.Shallow(ctx, ws.Root)
	if err != nil {
		return Result{}, err
	}
	resolution := coreidentity.ResolveProfile(s.profiles.Values, coreidentity.ResolutionInput{
		WorkspaceProfile:  s.profiles.WorkspaceBindings[string(ws.ID)],
		RepositoryProfile: s.profiles.RepositoryBindings[string(ws.RepositoryID)],
		Remote:            remote,
	})
	result := Result{WorkspaceID: string(ws.ID), RepositoryID: string(ws.RepositoryID), Effect: effect, Deep: deep, Resolution: resolution, Findings: []coreidentity.Finding{}}
	if resolution.ProfileName == "" {
		result.Findings = append(result.Findings, resolutionFinding(resolution))
		return result, nil
	}
	profile := s.profiles.Values[resolution.ProfileName]
	if deep {
		deepObs, deepErr := s.probe.Deep(ctx, ws.Root, profile, obs)
		if deepErr != nil {
			result.Findings = append(result.Findings, coreidentity.Finding{Code: "identity_observation_failed", Severity: "warning", Message: "deep identity observation was unavailable"})
		} else {
			obs = deepObs
		}
	}
	snapshot := coreidentity.Evaluate(resolution.ProfileName, resolution.Source, profile, obs)
	result.Snapshot = &snapshot
	result.Findings = append(result.Findings, coreidentity.Findings(snapshot)...)
	return result, nil
}

func resolutionFinding(resolution coreidentity.Resolution) coreidentity.Finding {
	code, message := "git_profile_unknown", "no Git identity profile matched the workspace"
	if resolution.Ambiguous {
		code, message = "git_profile_ambiguous", "multiple Git identity profiles matched; no profile was selected"
	} else if resolution.Missing {
		code, message = "git_profile_missing", "configured Git identity profile was not found"
	}
	return coreidentity.Finding{Code: code, Severity: "warning", Message: message, Facts: map[string]string{"resolution_source": resolution.Source}}
}
