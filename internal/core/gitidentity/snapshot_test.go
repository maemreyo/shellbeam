package gitidentity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityEvaluationSeparatesCommitTransportAndGitHub(t *testing.T) {
	profile := testProfiles()["work"]
	obs := Observation{
		Commit: CommitObservation{
			AuthorEmail:    "personal@example.invalid",
			CommitterEmail: "dev@company.example",
			Source:         CommitSourceWorktreeConfig,
		},
		Transport: TransportObservation{
			RemoteHost:   "github.com",
			RemoteOwner:  "company-org",
			SSHHostAlias: "github-personal",
			Override:     TransportOverrideNone,
		},
		GitHub: GitHubObservation{
			Host:             "github.com",
			StoredUser:       "personal-owner",
			EffectiveUser:    "personal-owner",
			CredentialSource: GHCredentialStored,
			TargetSource:     GHTargetRepository,
		},
	}
	got := Evaluate("work", "repository", profile, obs)
	if got.Commit.Author != MatchMismatch || got.Commit.Committer != MatchMatch {
		t.Fatalf("commit=%#v", got.Commit)
	}
	if got.Transport.RemoteOwnerMatch != MatchMatch || got.Transport.SSHAlias != MatchMismatch {
		t.Fatalf("transport=%#v", got.Transport)
	}
	if got.GitHub.User != MatchMismatch {
		t.Fatalf("github=%#v", got.GitHub)
	}
	codes := findingCodes(Findings(got))
	for _, want := range []string{"commit_identity_mismatch", "git_profile_mismatch", "gh_account_mismatch"} {
		if !codes[want] {
			t.Fatalf("findings=%v missing %s", codes, want)
		}
	}
}

func TestTokenAndTransportOverridesRemainPresenceOnly(t *testing.T) {
	profile := testProfiles()["work"]
	obs := Observation{
		Transport: TransportObservation{RemoteHost: "github.com", RemoteOwner: "company-org", Override: TransportOverrideGitSSHCommand},
		GitHub:    GitHubObservation{Host: "github.com", StoredUser: "dev-work", EffectiveUser: "", CredentialSource: GHCredentialTokenOverride, TargetSource: GHTargetEnvironment},
	}
	got := Evaluate("work", "repository", profile, obs)
	if got.GitHub.EffectiveUser != "" || got.GitHub.User != MatchUnknown {
		t.Fatalf("github=%#v", got.GitHub)
	}
	codes := findingCodes(Findings(got))
	if !codes["credential_env_override"] {
		t.Fatalf("findings=%v", codes)
	}
}

func TestIdentitySnapshotAndFindingsDoNotSerializeRawEmailOrSecretValues(t *testing.T) {
	profile := testProfiles()["work"]
	secretEmail := "very-secret-email@example.invalid"
	obs := Observation{Commit: CommitObservation{AuthorEmail: secretEmail, CommitterEmail: secretEmail, Source: CommitSourceEnvironment}}
	got := Evaluate("work", "workspace", profile, obs)
	data, err := json.Marshal(struct {
		Snapshot Snapshot  `json:"snapshot"`
		Findings []Finding `json:"findings"`
	}{got, Findings(got)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretEmail) || strings.Contains(string(data), "dev@company.example") {
		t.Fatalf("raw email leaked: %s", data)
	}
}

func findingCodes(findings []Finding) map[string]bool {
	out := map[string]bool{}
	for _, finding := range findings {
		out[finding.Code] = true
	}
	return out
}
