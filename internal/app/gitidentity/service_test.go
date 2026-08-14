package gitidentity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	coreidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeWorkspaceLookup struct {
	value workspace.Workspace
}

func (f fakeWorkspaceLookup) Inspect(context.Context, string) (workspace.Workspace, error) {
	return f.value, nil
}

type fakeProbe struct {
	shallow   coreidentity.Observation
	remote    coreidentity.RemoteIdentity
	deep      coreidentity.Observation
	deepErr   error
	deepCalls int
}

func (f *fakeProbe) Shallow(context.Context, string) (coreidentity.Observation, coreidentity.RemoteIdentity, error) {
	return f.shallow, f.remote, nil
}

func (f *fakeProbe) Deep(context.Context, string, coreidentity.Profile, coreidentity.Observation) (coreidentity.Observation, error) {
	f.deepCalls++
	return f.deep, f.deepErr
}

func testWorkspace() workspace.Workspace {
	return workspace.Workspace{ID: "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000", Root: "/repo"}
}

func testIdentityProfiles() Profiles {
	return Profiles{
		Values: map[string]coreidentity.Profile{
			"work": {RemoteOwners: []string{"company-org"}, CommitEmails: []string{"dev@company.example"}, GHHost: "github.com", GHUser: "dev-work"},
		},
		WorkspaceBindings: map[string]string{"ws_01K00000000000000000000000": "work"},
	}
}

func hasFinding(result Result, code string) bool {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestIdentityPreflightWorkspaceBindingWinsAndMismatchIsAdvisory(t *testing.T) {
	probe := &fakeProbe{
		shallow: coreidentity.Observation{Commit: coreidentity.CommitObservation{AuthorEmail: "personal@example.invalid", CommitterEmail: "dev@company.example", Source: coreidentity.CommitSourceWorktreeConfig}},
		remote:  coreidentity.RemoteIdentity{Host: "github.com", Owner: "personal-owner", Repository: "repo"},
	}
	service := New(fakeWorkspaceLookup{value: testWorkspace()}, probe, testIdentityProfiles())
	result, err := service.Preflight(context.Background(), "work", "push", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution.ProfileName != "work" || result.Resolution.Source != "workspace" {
		t.Fatalf("resolution=%#v", result.Resolution)
	}
	if !hasFinding(result, "commit_identity_mismatch") || probe.deepCalls != 0 {
		t.Fatalf("findings=%#v deepCalls=%d", result.Findings, probe.deepCalls)
	}
}

func TestIdentityPreflightUnknownProfileReturnsWarning(t *testing.T) {
	probe := &fakeProbe{remote: coreidentity.RemoteIdentity{Host: "example.com", Owner: "nobody", Repository: "repo"}}
	service := New(fakeWorkspaceLookup{value: testWorkspace()}, probe, Profiles{})
	result, err := service.Preflight(context.Background(), "work", "verify", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot != nil || !hasFinding(result, "git_profile_unknown") {
		t.Fatalf("result=%#v", result)
	}
}

func TestIdentityPreflightDeepFailureDegradesToAdvisory(t *testing.T) {
	shallow := coreidentity.Observation{Commit: coreidentity.CommitObservation{AuthorEmail: "dev@company.example", CommitterEmail: "dev@company.example", Source: coreidentity.CommitSourceConfig}}
	probe := &fakeProbe{shallow: shallow, deepErr: context.DeadlineExceeded}
	service := New(fakeWorkspaceLookup{value: testWorkspace()}, probe, testIdentityProfiles())
	result, err := service.Preflight(context.Background(), "work", "release", true)
	if err != nil {
		t.Fatal(err)
	}
	if probe.deepCalls != 1 || !hasFinding(result, "identity_observation_failed") {
		t.Fatalf("result=%#v deepCalls=%d", result, probe.deepCalls)
	}
	if result.Snapshot == nil || result.Snapshot.Commit.Author != coreidentity.MatchMatch {
		t.Fatalf("shallow observation was not preserved: %#v", result.Snapshot)
	}
}

func TestIdentityPreflightAcceptsAllDeclaredEffects(t *testing.T) {
	for _, effect := range []string{"push", "pr", "tag", "release", "publish", "verify"} {
		t.Run(effect, func(t *testing.T) {
			probe := &fakeProbe{}
			service := New(fakeWorkspaceLookup{value: testWorkspace()}, probe, testIdentityProfiles())
			result, err := service.Preflight(context.Background(), "work", effect, false)
			if err != nil {
				t.Fatalf("effect %s rejected: %v", effect, err)
			}
			if result.Effect != effect {
				t.Fatalf("effect=%q", result.Effect)
			}
		})
	}
}

func TestIdentityPreflightJSONDoesNotLeakRawIdentityValues(t *testing.T) {
	secretEmail := "secret-author@example.invalid"
	secretSigning := "PRIVATE-KEY-PATH-/Users/me/.ssh/id_work"
	probe := &fakeProbe{shallow: coreidentity.Observation{Commit: coreidentity.CommitObservation{AuthorEmail: secretEmail, CommitterEmail: secretEmail, SigningKeyFingerprint: secretSigning, Source: coreidentity.CommitSourceEnvironment}}}
	service := New(fakeWorkspaceLookup{value: testWorkspace()}, probe, testIdentityProfiles())
	result, err := service.Preflight(context.Background(), "work", "push", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secretEmail, secretSigning, "dev@company.example"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("identity value leaked: %s", data)
		}
	}
}

func TestIdentityPreflightRejectsUnknownEffect(t *testing.T) {
	service := New(fakeWorkspaceLookup{value: testWorkspace()}, &fakeProbe{}, testIdentityProfiles())
	if _, err := service.Preflight(context.Background(), "work", "deploy", false); err == nil {
		t.Fatal("unknown effect accepted")
	} else if !strings.Contains(err.Error(), "invalid identity preflight effect") {
		t.Fatalf("unexpected error: %v", err)
	}
}
