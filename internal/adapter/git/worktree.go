package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
)

func (r *Repository) ListWorktrees(ctx context.Context, commonDir string) ([]workspaceapp.GitWorktree, error) {
	commonDir, err := canonicalExisting(commonDir)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := r.runner.Run(ctx, "--git-dir", commonDir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %s", strings.TrimSpace(string(stderr)))
	}
	return parseWorktrees(stdout)
}

func (r *Repository) AddWorktree(ctx context.Context, commonDir, path, ref string) error {
	commonDir, err := canonicalExisting(commonDir)
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	args := []string{"--git-dir", commonDir, "worktree", "add", "--", filepath.Clean(path)}
	if ref != "" {
		args = append(args, ref)
	}
	_, stderr, err := r.runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(string(stderr)))
	}
	return nil
}

func (r *Repository) RemoveWorktree(ctx context.Context, commonDir, path string, force bool) error {
	args := []string{"--git-dir", commonDir, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, stderr, err := r.runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s", strings.TrimSpace(string(stderr)))
	}
	return nil
}

func (r *Repository) Dirty(ctx context.Context, path string) (bool, error) {
	path, err := canonicalInput(path)
	if err != nil {
		return false, err
	}
	stdout, stderr, err := r.runner.Run(ctx, "-C", path, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return false, fmt.Errorf("git status failed: %s", strings.TrimSpace(string(stderr)))
	}
	return len(stdout) != 0, nil
}

func parseWorktrees(data []byte) ([]workspaceapp.GitWorktree, error) {
	tokens := strings.Split(string(data), "\x00")
	out := make([]workspaceapp.GitWorktree, 0)
	var current workspaceapp.GitWorktree
	flush := func() {
		if current.Root != "" {
			out = append(out, current)
			current = workspaceapp.GitWorktree{}
		}
	}
	for _, token := range tokens {
		if token == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(token, "worktree "):
			if current.Root != "" {
				return nil, fmt.Errorf("invalid worktree porcelain")
			}
			current.Root = filepath.Clean(strings.TrimPrefix(token, "worktree "))
		case strings.HasPrefix(token, "HEAD "):
			current.Head = strings.TrimPrefix(token, "HEAD ")
		case strings.HasPrefix(token, "branch "):
			current.Branch = strings.TrimPrefix(token, "branch ")
		case token == "bare":
			current.Bare = true
		case token == "detached", token == "locked", strings.HasPrefix(token, "locked "), token == "prunable", strings.HasPrefix(token, "prunable "):
		default:
			return nil, fmt.Errorf("unknown worktree porcelain field")
		}
	}
	flush()
	return out, nil
}
