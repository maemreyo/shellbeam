package gitidentity

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxProfileTextBytes = 256

type Profile struct {
	SSHHostAliases         []string `json:"ssh_host_aliases,omitempty" toml:"ssh_host_aliases"`
	RemoteOwners           []string `json:"remote_owners,omitempty" toml:"remote_owners"`
	RemoteURLPatterns      []string `json:"remote_url_patterns,omitempty" toml:"remote_url_patterns"`
	CommitEmails           []string `json:"commit_emails,omitempty" toml:"commit_emails"`
	SigningKeyFingerprints []string `json:"signing_key_fingerprints,omitempty" toml:"signing_key_fingerprints"`
	GHHost                 string   `json:"gh_host,omitempty" toml:"gh_host"`
	GHUser                 string   `json:"gh_user,omitempty" toml:"gh_user"`
}

type RemoteIdentity struct {
	Host         string `json:"host,omitempty"`
	Owner        string `json:"owner,omitempty"`
	Repository   string `json:"repository,omitempty"`
	SSHHostAlias string `json:"ssh_host_alias,omitempty"`
}

func (r RemoteIdentity) Key() string {
	parts := []string{strings.ToLower(r.Host), strings.ToLower(r.Owner), strings.ToLower(r.Repository)}
	return strings.Join(parts, "/")
}

type ResolutionInput struct {
	WorkspaceProfile  string
	RepositoryProfile string
	Remote            RemoteIdentity
}

type Resolution struct {
	ProfileName string `json:"profile_name,omitempty"`
	Source      string `json:"source"`
	Ambiguous   bool   `json:"ambiguous"`
	Missing     bool   `json:"missing"`
}

func (p Profile) Validate() error {
	for _, values := range [][]string{p.SSHHostAliases, p.RemoteOwners, p.RemoteURLPatterns, p.CommitEmails, p.SigningKeyFingerprints} {
		if len(values) > 32 {
			return fmt.Errorf("too many Git identity profile rules")
		}
	}
	for _, values := range [][]string{p.SSHHostAliases, p.RemoteOwners, p.RemoteURLPatterns, p.CommitEmails, p.SigningKeyFingerprints} {
		for _, value := range values {
			if !safeProfileText(value) {
				return fmt.Errorf("invalid Git identity profile text")
			}
		}
	}
	for _, pattern := range p.RemoteURLPatterns {
		if _, err := path.Match(pattern, "github.com/owner/repo"); err != nil {
			return fmt.Errorf("invalid remote URL pattern")
		}
	}
	for _, email := range p.CommitEmails {
		if !validEmail(email) {
			return fmt.Errorf("invalid commit email")
		}
	}
	if (p.GHHost == "") != (p.GHUser == "") {
		return fmt.Errorf("GitHub host and user must be declared together")
	}
	if !safeProfileTextOptional(p.GHHost) || !safeProfileTextOptional(p.GHUser) {
		return fmt.Errorf("invalid GitHub profile text")
	}
	return nil
}

func ResolveProfile(profiles map[string]Profile, in ResolutionInput) Resolution {
	if in.WorkspaceProfile != "" {
		_, ok := profiles[in.WorkspaceProfile]
		return Resolution{ProfileName: valueIf(ok, in.WorkspaceProfile), Source: "workspace", Missing: !ok}
	}
	if in.RepositoryProfile != "" {
		_, ok := profiles[in.RepositoryProfile]
		return Resolution{ProfileName: valueIf(ok, in.RepositoryProfile), Source: "repository", Missing: !ok}
	}
	matches := make([]string, 0, len(profiles))
	for name, profile := range profiles {
		if profileMatchesRemote(profile, in.Remote) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return Resolution{Source: "unknown"}
	case 1:
		return Resolution{ProfileName: matches[0], Source: "remote_rule"}
	default:
		return Resolution{Source: "remote_rule", Ambiguous: true}
	}
}

func profileMatchesRemote(profile Profile, remote RemoteIdentity) bool {
	for _, owner := range profile.RemoteOwners {
		if remote.Owner != "" && strings.EqualFold(owner, remote.Owner) {
			return true
		}
	}
	for _, alias := range profile.SSHHostAliases {
		if remote.SSHHostAlias != "" && alias == remote.SSHHostAlias {
			return true
		}
	}
	key := remote.Key()
	for _, pattern := range profile.RemoteURLPatterns {
		matched, _ := path.Match(strings.ToLower(pattern), key)
		if matched {
			return true
		}
	}
	return false
}

func safeProfileText(value string) bool {
	return value != "" && safeProfileTextOptional(value)
}

func safeProfileTextOptional(value string) bool {
	if len(value) > MaxProfileTextBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validEmail(value string) bool {
	if !safeProfileText(value) || strings.Count(value, "@") != 1 || strings.ContainsAny(value, " \t") {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	return parts[0] != "" && strings.Contains(parts[1], ".")
}

func valueIf(ok bool, value string) string {
	if ok {
		return value
	}
	return ""
}
