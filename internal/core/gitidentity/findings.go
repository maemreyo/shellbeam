package gitidentity

type Finding struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"`
	Message  string            `json:"message"`
	Facts    map[string]string `json:"facts,omitempty"`
}

func Findings(snapshot Snapshot) []Finding {
	out := make([]Finding, 0, 5)
	if snapshot.Commit.Author == MatchMismatch || snapshot.Commit.Committer == MatchMismatch || snapshot.Commit.Signing == MatchMismatch {
		out = append(out, finding("commit_identity_mismatch", "effective Git commit identity does not match the expected profile", snapshot))
	}
	if snapshot.Transport.RemoteOwnerMatch == MatchMismatch || snapshot.Transport.SSHAlias == MatchMismatch {
		out = append(out, finding("git_profile_mismatch", "Git transport identity does not match the expected profile", snapshot))
	}
	if snapshot.Transport.Override != "" && snapshot.Transport.Override != TransportOverrideNone {
		out = append(out, finding("git_transport_override", "Git transport is affected by a runtime override", snapshot))
	}
	if snapshot.GitHub.User == MatchMismatch {
		out = append(out, finding("gh_account_mismatch", "GitHub CLI account does not match the expected profile", snapshot))
	}
	if snapshot.GitHub.CredentialSource == GHCredentialTokenOverride || snapshot.GitHub.CredentialSource == GHCredentialConfigOverride {
		out = append(out, finding("credential_env_override", "GitHub credentials are affected by an environment override", snapshot))
	}
	return out
}

func finding(code, message string, snapshot Snapshot) Finding {
	return Finding{
		Code:     code,
		Severity: "warning",
		Message:  message,
		Facts: map[string]string{
			"profile":           snapshot.ProfileName,
			"resolution_source": snapshot.ResolutionSource,
		},
	}
}
