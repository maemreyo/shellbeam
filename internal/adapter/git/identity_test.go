package git

import (
	"context"
	"errors"
	"fmt"
	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
	"strings"
	"testing"
)

type fakeIdentityRunner struct {
	calls     []string
	responses map[string]string
}

func (f *fakeIdentityRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for prefix, output := range f.responses {
		if strings.HasPrefix(call, prefix) {
			return []byte(output), nil, nil
		}
	}
	return nil, nil, fmt.Errorf("unexpected call: %s", call)
}

func TestIdentityDeepRunsExplicitSSHAndGitHubProbes(t *testing.T) {
	runner := &fakeIdentityRunner{responses: map[string]string{
		"git -C /repo config --worktree --get user.email": "dev@company.example\n",
		"git -C /repo config --get user.signingkey":       "ABC123\n",
		"git -C /repo remote get-url --push origin":       "git@github-work:company-org/repo.git\n",
		"ssh -G github-work":                              "hostname github.com\nuser git\nidentityfile /private/key\n",
		"gh auth status --active --hostname github.com":   "Logged in to github.com account dev-work (keyring)\n",
	}}
	probe := NewIdentityProbe(runner, func(string) string { return "" })
	shallow, _, err := probe.Shallow(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	profile := gitidentity.Profile{SSHHostAliases: []string{"github-work"}, GHHost: "github.com", GHUser: "dev-work"}
	deep, err := probe.Deep(context.Background(), "/repo", profile, shallow)
	if err != nil {
		t.Fatal(err)
	}
	if deep.GitHub.EffectiveUser != "dev-work" || deep.GitHub.CredentialSource != gitidentity.GHCredentialStored {
		t.Fatalf("github=%#v", deep.GitHub)
	}
	var sshCalls, ghCalls int
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "ssh ") {
			sshCalls++
		}
		if strings.HasPrefix(call, "gh ") {
			ghCalls++
		}
	}
	if sshCalls != 1 || ghCalls != 1 {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestIdentityShallowNeverInvokesSSHOrGitHub(t *testing.T) {
	runner := &fakeIdentityRunner{responses: map[string]string{
		"git -C /repo config --get user.signingkey": "ABC123\n",
		"git -C /repo remote get-url --push origin": "git@github-work:company-org/repo.git\n",
	}}
	env := map[string]string{
		"GIT_AUTHOR_EMAIL":    "dev@company.example",
		"GIT_COMMITTER_EMAIL": "dev@company.example",
		"GIT_SSH_COMMAND":     "ssh -i /private/secret-key",
		"GH_TOKEN":            "fake-token-that-must-not-leak",
		"GH_REPO":             "company-org/repo",
	}
	probe := NewIdentityProbe(runner, func(name string) string { return env[name] })
	obs, remote, err := probe.Shallow(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Commit.Source != gitidentity.CommitSourceEnvironment {
		t.Fatalf("commit source=%s", obs.Commit.Source)
	}
	if obs.Transport.Override != gitidentity.TransportOverrideGitSSHCommand {
		t.Fatalf("transport=%#v", obs.Transport)
	}
	if obs.GitHub.CredentialSource != gitidentity.GHCredentialTokenOverride || obs.GitHub.TargetSource != gitidentity.GHTargetEnvironment {
		t.Fatalf("github=%#v", obs.GitHub)
	}
	if remote.Owner != "company-org" || remote.SSHHostAlias != "github-work" {
		t.Fatalf("remote=%#v", remote)
	}
	for _, call := range runner.calls {
		if !strings.HasPrefix(call, "git ") {
			t.Fatalf("shallow invoked external identity probe: %q", call)
		}
	}
}

type failingDeepRunner struct {
	sawDeadline bool
	err         error
}

func (r *failingDeepRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, []byte, error) {
	_, r.sawDeadline = ctx.Deadline()
	return nil, []byte("hostile TOKEN=secret /Users/me/.ssh/id_work"), r.err
}

func TestIdentityDeepProbeIsBoundedAndSanitizesFailure(t *testing.T) {
	runner := &failingDeepRunner{err: context.DeadlineExceeded}
	probe := NewIdentityProbe(runner, func(string) string { return "" })
	current := gitidentity.Observation{
		Transport: gitidentity.TransportObservation{SSHHostAlias: "github-work"},
		GitHub:    gitidentity.GitHubObservation{Host: "github.com"},
	}
	_, err := probe.Deep(context.Background(), "/repo", gitidentity.Profile{GHHost: "github.com", GHUser: "work"}, current)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deep error=%v", err)
	}
	if !runner.sawDeadline {
		t.Fatal("deep probe ran without a deadline")
	}
	for _, forbidden := range []string{"TOKEN=secret", "/Users/me/.ssh/id_work"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("deep error leaked hostile output: %q", err)
		}
	}
}

type deadlineIdentityRunner struct {
	inner      *fakeIdentityRunner
	allBounded bool
}

func (r *deadlineIdentityRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		r.allBounded = false
	}
	return r.inner.Run(ctx, name, args...)
}

func TestIdentityShallowGitProbesAreBounded(t *testing.T) {
	inner := &fakeIdentityRunner{responses: map[string]string{
		"git -C /repo config --get user.signingkey": "ABC123",
		"git -C /repo remote get-url --push origin": "git@github-work:company-org/repo.git",
	}}
	runner := &deadlineIdentityRunner{inner: inner, allBounded: true}
	probe := NewIdentityProbe(runner, func(name string) string {
		if name == "GIT_AUTHOR_EMAIL" || name == "GIT_COMMITTER_EMAIL" {
			return "dev@company.example"
		}
		return ""
	})
	if _, _, err := probe.Shallow(context.Background(), "/repo"); err != nil {
		t.Fatal(err)
	}
	if !runner.allBounded {
		t.Fatal("shallow Git probe ran without a deadline")
	}
}

func TestOSIdentityRunnerBoundsCapturedOutput(t *testing.T) {
	runner := OSIdentityRunner{}
	command := "i=0; while [ $i -lt 5000 ]; do printf 0123456789; printf abcdefghij >&2; i=$((i+1)); done"
	stdout, stderr, err := runner.Run(context.Background(), "/bin/sh", "-c", command)
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout) > maxIdentityProbeOutputBytes || len(stderr) > maxIdentityProbeOutputBytes {
		t.Fatalf("unbounded identity output: stdout=%d stderr=%d", len(stdout), len(stderr))
	}
}
