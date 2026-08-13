package workspace

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func repositoryByCommonDir(records []core.Repository, commonDir string) (core.Repository, bool) {
	for _, record := range records {
		if filepath.Clean(record.CommonDir) == filepath.Clean(commonDir) {
			return record, true
		}
	}
	return core.Repository{}, false
}

func repositoryByID(records []core.Repository, id core.RepositoryID) (core.Repository, bool) {
	for _, record := range records {
		if record.ID == id {
			return record, true
		}
	}
	return core.Repository{}, false
}

func workspaceByGitDir(records []core.Workspace, gitDir string) (core.Workspace, bool) {
	for _, record := range records {
		if filepath.Clean(record.GitDir) == filepath.Clean(gitDir) {
			return record, true
		}
	}
	return core.Workspace{}, false
}

func resolveWorkspace(records []core.Workspace, labelOrID string) (core.Workspace, error) {
	for _, record := range records {
		if string(record.ID) == labelOrID || record.Label == labelOrID {
			return record, nil
		}
	}
	return core.Workspace{}, ErrWorkspaceNotFound
}

func resolvedLabel(label string, id core.WorkspaceID, records []core.Workspace) string {
	if !labelTaken(label, id, records) {
		return label
	}
	raw := strings.TrimPrefix(string(id), "ws_")
	if len(raw) > 6 {
		raw = raw[len(raw)-6:]
	}
	base := label + "-" + strings.ToLower(raw)
	if !labelTaken(base, id, records) {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !labelTaken(candidate, id, records) {
			return candidate
		}
	}
}

func labelTaken(label string, id core.WorkspaceID, records []core.Workspace) bool {
	for _, record := range records {
		if record.ID != id && record.Label == label {
			return true
		}
	}
	return false
}

func derivedLabel(observation GitObservation) string {
	if observation.Branch != "" {
		return labelFromRef(observation.Branch)
	}
	if base := filepath.Base(observation.Root); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "workspace"
}

func labelFromRef(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimSuffix(ref, "/")
	if ref == "" {
		return ""
	}
	return filepath.Base(ref)
}

func worktreeByRef(worktrees []GitWorktree, ref string) (GitWorktree, bool) {
	if ref == "" {
		return GitWorktree{}, false
	}
	branch := ref
	if !strings.HasPrefix(branch, "refs/") {
		branch = "refs/heads/" + branch
	}
	for _, worktree := range worktrees {
		if worktree.Branch == branch || worktree.Head == ref {
			return worktree, true
		}
	}
	return GitWorktree{}, false
}

func worktreeByRoot(worktrees []GitWorktree, root string) (GitWorktree, bool) {
	root = filepath.Clean(root)
	for _, worktree := range worktrees {
		if filepath.Clean(worktree.Root) == root {
			return worktree, true
		}
	}
	return GitWorktree{}, false
}

func defaultOrExplicitTarget(observation GitObservation, explicit, label string) (string, error) {
	if explicit != "" {
		return absoluteClean(explicit)
	}
	return filepath.Join(
		filepath.Dir(observation.Root),
		filepath.Base(observation.Root)+"-worktrees",
		pathLabel(label),
	), nil
}

func availableTarget(base string, explicit bool, commonDir, ref, label string) (string, error) {
	if !occupied(base) {
		return base, nil
	}
	if explicit {
		return "", fmt.Errorf("%w: explicit path is occupied", ErrUnsafeWorktree)
	}
	sum := sha256.Sum256([]byte(commonDir + "\x00" + ref + "\x00" + label))
	suffix := fmt.Sprintf("%x", sum[:4])
	for i := 0; i < 100; i++ {
		candidate := base + "-" + suffix
		if i > 0 {
			candidate = fmt.Sprintf("%s-%s-%d", base, suffix, i+1)
		}
		if !occupied(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: no safe worktree path available", ErrUnsafeWorktree)
}

func absoluteClean(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func occupied(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func pathLabel(label string) string {
	label = strings.TrimSpace(label)
	label = strings.ReplaceAll(label, string(filepath.Separator), "_")
	if label == "" || label == "." || label == ".." {
		return "workspace"
	}
	return label
}
