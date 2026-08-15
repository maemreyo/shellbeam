package evidence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errArtifactMissing = errors.New("artifact path missing")
	errArtifactEscape  = errors.New("artifact path escapes workspace")
)

func canonicalWorkspaceRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("workspace root must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace root unavailable")
	}
	return resolved, nil
}

func containedArtifactPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." {
		return root, nil
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := root
	for _, part := range parts[:len(parts)-1] {
		candidate := filepath.Join(current, part)
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return "", errArtifactMissing
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", err
			}
			if !pathContained(root, resolved) {
				return "", errArtifactEscape
			}
			current = resolved
			info, err = os.Stat(current)
			if err != nil {
				return "", err
			}
		} else {
			current = candidate
		}
		if !info.IsDir() {
			return "", fmt.Errorf("artifact parent is not a directory")
		}
	}
	target := filepath.Join(current, parts[len(parts)-1])
	if !pathContained(root, target) {
		return "", errArtifactEscape
	}
	return target, nil
}

func pathContained(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
