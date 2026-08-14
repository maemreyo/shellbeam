package git

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
)

type IdentityCommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, []byte, error)
}

const (
	identityProbeTimeout        = 2 * time.Second
	maxIdentityProbeOutputBytes = 32 << 10
)

type boundedIdentityBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *boundedIdentityBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.limit <= 0 || b.buf.Len() >= b.limit {
		return original, nil
	}
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return original, nil
}

type OSIdentityRunner struct{}

func (OSIdentityRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := boundedIdentityBuffer{limit: maxIdentityProbeOutputBytes}
	stderr := boundedIdentityBuffer{limit: maxIdentityProbeOutputBytes}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.buf.Bytes(), stderr.buf.Bytes(), err
}

type IdentityProbe struct {
	runner IdentityCommandRunner
	getenv func(string) string
}

func NewIdentityProbe(runner IdentityCommandRunner, getenv func(string) string) *IdentityProbe {
	if runner == nil {
		runner = OSIdentityRunner{}
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	return &IdentityProbe{runner: runner, getenv: getenv}
}

func (p *IdentityProbe) Shallow(ctx context.Context, root string) (gitidentity.Observation, gitidentity.RemoteIdentity, error) {
	commit := p.commitObservation(ctx, root)
	remoteURL := p.optional(ctx, "git", "-C", root, "remote", "get-url", "--push", "origin")
	if remoteURL == "" {
		remoteURL = p.optional(ctx, "git", "-C", root, "remote", "get-url", "origin")
	}
	remote := parseRemoteIdentity(remoteURL)
	transport := gitidentity.TransportObservation{
		RemoteHost: remote.Host, RemoteOwner: remote.Owner, SSHHostAlias: remote.SSHHostAlias,
		Override: gitidentity.TransportOverrideNone,
	}
	if p.getenv("GIT_SSH_COMMAND") != "" {
		transport.Override = gitidentity.TransportOverrideGitSSHCommand
	} else if p.getenv("GIT_SSH") != "" {
		transport.Override = gitidentity.TransportOverrideGitSSH
	}

	host := strings.TrimSpace(p.getenv("GH_HOST"))
	targetSource := gitidentity.GHTargetUnknown
	if host == "" {
		host = remote.Host
		if host != "" {
			targetSource = gitidentity.GHTargetRepository
		}
	}
	if p.getenv("GH_HOST") != "" || p.getenv("GH_REPO") != "" || p.getenv("GH_CONFIG_DIR") != "" {
		targetSource = gitidentity.GHTargetEnvironment
	}
	credentialSource := gitidentity.GHCredentialUnknown
	if p.hasAnyEnv("GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN") {
		credentialSource = gitidentity.GHCredentialTokenOverride
	} else if p.getenv("GH_CONFIG_DIR") != "" {
		credentialSource = gitidentity.GHCredentialConfigOverride
	}
	obs := gitidentity.Observation{
		Commit:    commit,
		Transport: transport,
		GitHub: gitidentity.GitHubObservation{
			Host: host, CredentialSource: credentialSource, TargetSource: targetSource,
		},
	}
	return obs, remote, nil
}

func (p *IdentityProbe) optional(ctx context.Context, name string, args ...string) string {
	probeCtx, cancel := context.WithTimeout(ctx, identityProbeTimeout)
	defer cancel()
	stdout, _, err := p.runner.Run(probeCtx, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(stdout))
}

func (p *IdentityProbe) hasAnyEnv(names ...string) bool {
	for _, name := range names {
		if p.getenv(name) != "" {
			return true
		}
	}
	return false
}

func (p *IdentityProbe) commitObservation(ctx context.Context, root string) gitidentity.CommitObservation {
	author := p.getenv("GIT_AUTHOR_EMAIL")
	committer := p.getenv("GIT_COMMITTER_EMAIL")
	fallback := p.getenv("EMAIL")
	if author == "" {
		author = fallback
	}
	if committer == "" {
		committer = fallback
	}
	source := gitidentity.CommitSourceEnvironment
	if author == "" && committer == "" {
		email := p.optional(ctx, "git", "-C", root, "config", "--worktree", "--get", "user.email")
		source = gitidentity.CommitSourceWorktreeConfig
		if email == "" {
			email = p.optional(ctx, "git", "-C", root, "config", "--get", "user.email")
			source = gitidentity.CommitSourceConfig
		}
		if p.hasAnyEnv("GIT_CONFIG_COUNT", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM") {
			source = gitidentity.CommitSourceRuntimeConfig
		}
		author, committer = email, email
	}
	signing := p.optional(ctx, "git", "-C", root, "config", "--get", "user.signingkey")
	return gitidentity.CommitObservation{AuthorEmail: author, CommitterEmail: committer, SigningKeyFingerprint: signing, Source: source}
}

var errIdentityDeepObservationUnavailable = errors.New("identity deep observation unavailable")

func (p *IdentityProbe) Deep(ctx context.Context, root string, profile gitidentity.Profile, current gitidentity.Observation) (gitidentity.Observation, error) {
	probeCtx, cancel := context.WithTimeout(ctx, identityProbeTimeout)
	defer cancel()
	out := current
	if alias := current.Transport.SSHHostAlias; alias != "" {
		if _, _, err := p.runner.Run(probeCtx, "ssh", "-G", alias); err != nil {
			return out, sanitizeDeepObservationError(err)
		}
	}
	if out.GitHub.CredentialSource == gitidentity.GHCredentialTokenOverride {
		out.GitHub.EffectiveUser = ""
		return out, nil
	}
	host := profile.GHHost
	if host == "" {
		host = out.GitHub.Host
	}
	if host == "" {
		return out, nil
	}
	stdout, stderr, err := p.runner.Run(probeCtx, "gh", "auth", "status", "--active", "--hostname", host)
	if err != nil {
		return out, sanitizeDeepObservationError(err)
	}
	user := parseGHAccount(string(stdout) + "\n" + string(stderr))
	if user != "" {
		out.GitHub.Host = host
		out.GitHub.StoredUser = user
		out.GitHub.EffectiveUser = user
		if out.GitHub.CredentialSource != gitidentity.GHCredentialConfigOverride {
			out.GitHub.CredentialSource = gitidentity.GHCredentialStored
		}
	}
	return out, nil
}

func sanitizeDeepObservationError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		return errIdentityDeepObservationUnavailable
	}
}

func parseRemoteIdentity(raw string) gitidentity.RemoteIdentity {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return gitidentity.RemoteIdentity{}
	}
	host, pathPart, sshAlias := "", "", ""
	if !strings.Contains(raw, "://") {
		if colon := strings.Index(raw, ":"); colon > 0 {
			hostPart := raw[:colon]
			pathPart = raw[colon+1:]
			if at := strings.LastIndex(hostPart, "@"); at >= 0 {
				hostPart = hostPart[at+1:]
			}
			host, sshAlias = hostPart, hostPart
		}
	}
	if host == "" {
		if parsed, err := url.Parse(raw); err == nil {
			host = parsed.Hostname()
			pathPart = strings.TrimPrefix(parsed.Path, "/")
			if parsed.Scheme == "ssh" {
				sshAlias = host
			}
		}
	}
	pathPart = strings.TrimSuffix(pathPart, ".git")
	parts := strings.Split(strings.Trim(pathPart, "/"), "/")
	out := gitidentity.RemoteIdentity{Host: strings.ToLower(host), SSHHostAlias: sshAlias}
	if len(parts) > 0 {
		out.Owner = parts[0]
	}
	if len(parts) > 1 {
		out.Repository = parts[1]
	}
	return out
}

func parseGHAccount(text string) string {
	marker := "account "
	index := strings.Index(text, marker)
	if index < 0 {
		return ""
	}
	rest := text[index+len(marker):]
	if end := strings.IndexAny(rest, " \t\r\n("); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
