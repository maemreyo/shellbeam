package gitidentity

type GitHubSnapshot struct {
	Host             string             `json:"host,omitempty"`
	StoredUser       string             `json:"stored_user,omitempty"`
	EffectiveUser    string             `json:"effective_user,omitempty"`
	User             MatchStatus        `json:"user"`
	CredentialSource GHCredentialSource `json:"credential_source"`
	TargetSource     GHTargetSource     `json:"target_source"`
}

type Snapshot struct {
	SchemaVersion    int               `json:"schema_version"`
	ProfileName      string            `json:"profile_name"`
	ResolutionSource string            `json:"resolution_source"`
	Commit           CommitSnapshot    `json:"commit"`
	Transport        TransportSnapshot `json:"transport"`
	GitHub           GitHubSnapshot    `json:"github"`
}
