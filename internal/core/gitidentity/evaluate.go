package gitidentity

import "strings"

const SnapshotSchemaVersion = 1

type CommitSnapshot struct {
	Author    MatchStatus  `json:"author"`
	Committer MatchStatus  `json:"committer"`
	Signing   MatchStatus  `json:"signing"`
	Source    CommitSource `json:"source"`
}

type TransportSnapshot struct {
	RemoteHost       string            `json:"remote_host,omitempty"`
	RemoteOwner      string            `json:"remote_owner,omitempty"`
	SSHHostAlias     string            `json:"ssh_host_alias,omitempty"`
	RemoteOwnerMatch MatchStatus       `json:"remote_owner_match"`
	SSHAlias         MatchStatus       `json:"ssh_alias"`
	Override         TransportOverride `json:"override"`
}

func Evaluate(profileName, resolutionSource string, profile Profile, obs Observation) Snapshot {
	effectiveUser := obs.GitHub.EffectiveUser
	if obs.GitHub.CredentialSource == GHCredentialTokenOverride {
		effectiveUser = ""
	}
	return Snapshot{
		SchemaVersion:    SnapshotSchemaVersion,
		ProfileName:      profileName,
		ResolutionSource: resolutionSource,
		Commit: CommitSnapshot{
			Author:    matchFold(obs.Commit.AuthorEmail, profile.CommitEmails),
			Committer: matchFold(obs.Commit.CommitterEmail, profile.CommitEmails),
			Signing:   matchFold(obs.Commit.SigningKeyFingerprint, profile.SigningKeyFingerprints),
			Source:    obs.Commit.Source,
		},
		Transport: TransportSnapshot{
			RemoteHost:       obs.Transport.RemoteHost,
			RemoteOwner:      obs.Transport.RemoteOwner,
			SSHHostAlias:     obs.Transport.SSHHostAlias,
			RemoteOwnerMatch: matchFold(obs.Transport.RemoteOwner, profile.RemoteOwners),
			SSHAlias:         matchExact(obs.Transport.SSHHostAlias, profile.SSHHostAliases),
			Override:         obs.Transport.Override,
		},
		GitHub: GitHubSnapshot{
			Host:             obs.GitHub.Host,
			StoredUser:       obs.GitHub.StoredUser,
			EffectiveUser:    effectiveUser,
			User:             matchFold(effectiveUser, []string{profile.GHUser}),
			CredentialSource: obs.GitHub.CredentialSource,
			TargetSource:     obs.GitHub.TargetSource,
		},
	}
}

func matchFold(actual string, expected []string) MatchStatus {
	if actual == "" || len(expected) == 0 {
		return MatchUnknown
	}
	for _, value := range expected {
		if strings.EqualFold(actual, value) {
			return MatchMatch
		}
	}
	return MatchMismatch
}

func matchExact(actual string, expected []string) MatchStatus {
	if actual == "" || len(expected) == 0 {
		return MatchUnknown
	}
	for _, value := range expected {
		if actual == value {
			return MatchMatch
		}
	}
	return MatchMismatch
}
