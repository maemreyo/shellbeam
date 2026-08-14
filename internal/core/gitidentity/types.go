package gitidentity

type MatchStatus string

const (
	MatchUnknown  MatchStatus = "unknown"
	MatchMatch    MatchStatus = "match"
	MatchMismatch MatchStatus = "mismatch"
)

type CommitSource string

const (
	CommitSourceUnknown        CommitSource = "unknown"
	CommitSourceConfig         CommitSource = "config"
	CommitSourceWorktreeConfig CommitSource = "worktree_config"
	CommitSourceEnvironment    CommitSource = "environment"
	CommitSourceRuntimeConfig  CommitSource = "runtime_config"
)

type TransportOverride string

const (
	TransportOverrideNone          TransportOverride = "none"
	TransportOverrideGitSSH        TransportOverride = "git_ssh"
	TransportOverrideGitSSHCommand TransportOverride = "git_ssh_command"
)

type GHCredentialSource string

const (
	GHCredentialUnknown        GHCredentialSource = "unknown"
	GHCredentialStored         GHCredentialSource = "stored"
	GHCredentialConfigOverride GHCredentialSource = "config_override"
	GHCredentialTokenOverride  GHCredentialSource = "token_override"
)

type GHTargetSource string

const (
	GHTargetUnknown     GHTargetSource = "unknown"
	GHTargetRepository  GHTargetSource = "repository"
	GHTargetEnvironment GHTargetSource = "environment"
)

type CommitObservation struct {
	AuthorEmail           string       `json:"-"`
	CommitterEmail        string       `json:"-"`
	SigningKeyFingerprint string       `json:"-"`
	Source                CommitSource `json:"source"`
}

type TransportObservation struct {
	RemoteHost   string            `json:"remote_host,omitempty"`
	RemoteOwner  string            `json:"remote_owner,omitempty"`
	SSHHostAlias string            `json:"ssh_host_alias,omitempty"`
	Override     TransportOverride `json:"override"`
}

type GitHubObservation struct {
	Host             string             `json:"host,omitempty"`
	StoredUser       string             `json:"stored_user,omitempty"`
	EffectiveUser    string             `json:"effective_user,omitempty"`
	CredentialSource GHCredentialSource `json:"credential_source"`
	TargetSource     GHTargetSource     `json:"target_source"`
}

type Observation struct {
	Commit    CommitObservation    `json:"-"`
	Transport TransportObservation `json:"transport"`
	GitHub    GitHubObservation    `json:"github"`
}
