// Package git implements Git-native repository and worktree observation.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
)

const maxGitOutputBytes = 1 << 20

type commandRunner interface {
	Run(context.Context, ...string) ([]byte, []byte, error)
}

type boundedCommandRunner interface {
	RunBounded(context.Context, int, ...string) ([]byte, []byte, error)
}

type Repository struct {
	runner          commandRunner
	snapshotOptions SnapshotOptions
	snapshots       *snapshotCache
}

func New() *Repository { return newRepository(execRunner{}, SnapshotOptions{}) }

func (r *Repository) Inspect(ctx context.Context, path string) (workspaceapp.GitObservation, error) {
	root, err := canonicalInput(path)
	if err != nil {
		return workspaceapp.GitObservation{}, err
	}
	commonDir, err := r.revParsePath(ctx, root, "--git-common-dir")
	if err != nil {
		return workspaceapp.GitObservation{}, err
	}
	gitDir, err := r.revParsePath(ctx, root, "--git-dir")
	if err != nil {
		return workspaceapp.GitObservation{}, err
	}
	bareText, err := r.gitText(ctx, "-C", root, "rev-parse", "--is-bare-repository")
	if err != nil {
		return workspaceapp.GitObservation{}, err
	}
	bare := bareText == "true"
	worktreeRoot := root
	branch := ""
	if !bare {
		worktreeRoot, err = r.revParsePath(ctx, root, "--show-toplevel")
		if err != nil {
			return workspaceapp.GitObservation{}, err
		}
		branch, err = r.gitText(ctx, "-C", worktreeRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return workspaceapp.GitObservation{}, err
		}
		if branch == "HEAD" {
			branch = ""
		}
	}
	return workspaceapp.GitObservation{CommonDir: commonDir, Root: worktreeRoot, GitDir: gitDir, Branch: branch, Bare: bare}, nil
}

func (r *Repository) revParsePath(ctx context.Context, cwd, field string) (string, error) {
	text, err := r.gitText(ctx, "-C", cwd, "rev-parse", "--path-format=absolute", field)
	if err != nil {
		return "", err
	}
	return canonicalExisting(text)
}

func (r *Repository) gitText(ctx context.Context, args ...string) (string, error) {
	stdout, stderr, err := r.runner.Run(ctx, args...)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git command failed: %s", message)
	}
	return strings.TrimSpace(string(stdout)), nil
}

func canonicalInput(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return canonicalExisting(abs)
}

func canonicalExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

type execRunner struct{}

func (r execRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	return r.RunBounded(ctx, maxGitOutputBytes, args...)
}

func (execRunner) RunBounded(ctx context.Context, limit int, args ...string) ([]byte, []byte, error) {
	if limit < 1 || limit > maxGitOutputBytes {
		limit = maxGitOutputBytes
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	stdout := newCappedBuffer(limit)
	stderr := newCappedBuffer(limit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(stdout.err, errOutputLimit) || errors.Is(stderr.err, errOutputLimit) {
		return stdout.Bytes(), stderr.Bytes(), errOutputLimit
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func runGitBounded(runner commandRunner, ctx context.Context, limit int, args ...string) ([]byte, []byte, error) {
	if bounded, ok := runner.(boundedCommandRunner); ok {
		return bounded.RunBounded(ctx, limit, args...)
	}
	stdout, stderr, err := runner.Run(ctx, args...)
	if len(stdout) > limit {
		return append([]byte(nil), stdout[:limit]...), stderr, errOutputLimit
	}
	return stdout, stderr, err
}

var errOutputLimit = errors.New("git output limit exceeded")

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	err   error
}

func newCappedBuffer(limit int) *cappedBuffer { return &cappedBuffer{limit: limit} }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.err = errOutputLimit
		return remaining, b.err
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte { return append([]byte(nil), b.buf.Bytes()...) }

func (r *Repository) ResolveWorktreeRoot(ctx context.Context, gitDir string) (string, error) {
	gitDir, err := canonicalExisting(gitDir)
	if err != nil {
		return "", err
	}
	candidates := worktreeRootCandidates(gitDir)
	if stdout, _, configErr := r.runner.Run(ctx, "--git-dir", gitDir, "config", "--path", "--get", "core.worktree"); configErr == nil {
		root := strings.TrimSpace(string(stdout))
		if root != "" {
			if !filepath.IsAbs(root) {
				root = filepath.Join(gitDir, root)
			}
			candidates = append(candidates, root)
		}
	}
	for _, candidate := range candidates {
		if root, ok := r.verifiedWorktreeRoot(ctx, gitDir, candidate); ok {
			return root, nil
		}
	}
	if stdout, _, bareErr := r.runner.Run(ctx, "--git-dir", gitDir, "rev-parse", "--is-bare-repository"); bareErr == nil && strings.TrimSpace(string(stdout)) == "true" {
		return gitDir, nil
	}
	return "", fmt.Errorf("git worktree root unavailable")
}

func worktreeRootCandidates(gitDir string) []string {
	var candidates []string
	if data, err := os.ReadFile(filepath.Join(gitDir, "gitdir")); err == nil && len(data) <= 4096 {
		pointer := strings.TrimSpace(string(data))
		if filepath.IsAbs(pointer) && filepath.Base(pointer) == ".git" {
			candidates = append(candidates, filepath.Dir(pointer))
		}
	}
	if filepath.Base(gitDir) == ".git" {
		candidates = append(candidates, filepath.Dir(gitDir))
	}
	return candidates
}

func (r *Repository) verifiedWorktreeRoot(ctx context.Context, gitDir, candidate string) (string, bool) {
	root, err := canonicalExisting(candidate)
	if err != nil {
		return "", false
	}
	stdout, _, err := r.runner.Run(ctx, "-C", root, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return "", false
	}
	observed, err := canonicalExisting(strings.TrimSpace(string(stdout)))
	if err != nil || observed != gitDir {
		return "", false
	}
	return root, true
}
