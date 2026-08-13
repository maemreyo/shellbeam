// Package git implements Git-native repository and worktree observation.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
)

const maxGitOutputBytes = 1 << 20

type commandRunner interface {
	Run(context.Context, ...string) ([]byte, []byte, error)
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

func (execRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	stdout := newCappedBuffer(maxGitOutputBytes)
	stderr := newCappedBuffer(maxGitOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(stdout.err, errOutputLimit) || errors.Is(stderr.err, errOutputLimit) {
		return stdout.Bytes(), stderr.Bytes(), errOutputLimit
	}
	return stdout.Bytes(), stderr.Bytes(), err
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
