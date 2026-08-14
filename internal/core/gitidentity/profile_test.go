package gitidentity

import "testing"

func testProfiles() map[string]Profile {
	return map[string]Profile{
		"work": {
			SSHHostAliases:    []string{"github-work"},
			RemoteOwners:      []string{"company-org"},
			RemoteURLPatterns: []string{"github.com/company-org/*"},
			CommitEmails:      []string{"dev@company.example"},
			GHHost:            "github.com",
			GHUser:            "dev-work",
		},
		"personal": {
			SSHHostAliases: []string{"github-personal"},
			RemoteOwners:   []string{"personal-owner"},
			CommitEmails:   []string{"me@personal.example"},
			GHHost:         "github.com",
			GHUser:         "personal-owner",
		},
	}
}

func TestProfileResolutionOrderAndUniqueRemoteMatch(t *testing.T) {
	profiles := testProfiles()
	remote := RemoteIdentity{Host: "github.com", Owner: "company-org", Repository: "repo", SSHHostAlias: "github-work"}
	cases := []struct {
		name                 string
		in                   ResolutionInput
		wantName, wantSource string
	}{
		{"workspace", ResolutionInput{WorkspaceProfile: "personal", RepositoryProfile: "work", Remote: remote}, "personal", "workspace"},
		{"repository", ResolutionInput{RepositoryProfile: "work", Remote: RemoteIdentity{Host: "github.com", Owner: "personal-owner", Repository: "repo"}}, "work", "repository"},
		{"remote", ResolutionInput{Remote: remote}, "work", "remote_rule"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveProfile(profiles, tc.in)
			if got.ProfileName != tc.wantName || got.Source != tc.wantSource || got.Ambiguous {
				t.Fatalf("got=%#v", got)
			}
		})
	}
}

func TestProfileResolutionAmbiguousAndUnknownDoNotChooseSilently(t *testing.T) {
	profiles := testProfiles()
	p := profiles["personal"]
	p.RemoteOwners = []string{"company-org"}
	profiles["personal"] = p
	ambiguous := ResolveProfile(profiles, ResolutionInput{Remote: RemoteIdentity{Host: "github.com", Owner: "company-org", Repository: "repo"}})
	if ambiguous.ProfileName != "" || !ambiguous.Ambiguous || ambiguous.Source != "remote_rule" {
		t.Fatalf("ambiguous=%#v", ambiguous)
	}
	unknown := ResolveProfile(profiles, ResolutionInput{Remote: RemoteIdentity{Host: "example.com", Owner: "nobody", Repository: "repo"}})
	if unknown.ProfileName != "" || unknown.Ambiguous || unknown.Source != "unknown" {
		t.Fatalf("unknown=%#v", unknown)
	}
}

func TestProfileValidationRejectsUnsafeRules(t *testing.T) {
	good := testProfiles()["work"]
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []Profile{
		{SSHHostAliases: []string{"bad\nalias"}},
		{RemoteURLPatterns: []string{"[bad"}},
		{CommitEmails: []string{"not-an-email"}},
		{GHHost: "github.com", GHUser: ""},
	}
	for _, p := range invalid {
		if err := p.Validate(); err == nil {
			t.Fatalf("profile %#v accepted", p)
		}
	}
}
