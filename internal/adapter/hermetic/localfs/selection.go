//go:build linux || darwin

package localfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

type selectedFile struct {
	Path string
}

func canonicalSelectors(values []string) ([]string, error) {
	request := core.Request{
		Version: core.RequestVersionV1, Mode: core.ModeRequired, RepoInputs: values,
		Network: core.NetworkOff, Environment: core.EnvironmentFixedAllowlist,
		Stdin: core.StdinClosed, Writes: core.WritesEphemeralDiscard,
	}
	canonical, err := request.Canonical()
	if err != nil {
		return nil, err
	}
	return canonical.RepoInputs, nil
}

func selectSourceFiles(ctx context.Context, root string, selectors []string, limits core.CaptureLimits) ([]selectedFile, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	selected := make(map[string]selectedFile)
	walked := 0
	for _, raw := range selectors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selector, err := core.ParseRepoInputSelector(raw)
		if err != nil {
			return nil, err
		}
		if selector.Recursive {
			if err := walkSelector(ctx, root, selector.Path, limits, &walked, selected); err != nil {
				return nil, err
			}
			continue
		}
		walked++
		if walked > limits.MaxWalkEntries {
			return nil, fmt.Errorf("hermetic capture walk budget exceeded")
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(selector.Path)))
		if err != nil {
			return nil, err
		}
		if err := selectRegular(selector.Path, info, selected, limits); err != nil {
			return nil, err
		}
	}
	out := make([]selectedFile, 0, len(selected))
	for _, entry := range selected {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > limits.MaxPaths {
		return nil, fmt.Errorf("hermetic capture path budget exceeded")
	}
	return out, nil
}

func walkSelector(ctx context.Context, root, rel string, limits core.CaptureLimits, walked *int, selected map[string]selectedFile) error {
	start := filepath.Join(root, filepath.FromSlash(rel))
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		*walked++
		if *walked > limits.MaxWalkEntries {
			return fmt.Errorf("hermetic capture walk budget exceeded")
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		repoPath := filepath.ToSlash(relPath)
		if hasGitComponent(repoPath) {
			return fmt.Errorf("git metadata is not a hermetic repo input")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return selectRegular(repoPath, info, selected, limits)
	})
}

func selectRegular(path string, info os.FileInfo, selected map[string]selectedFile, limits core.CaptureLimits) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink inputs are unsupported")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("special or directory input is unsupported")
	}
	if info.Size() < 0 || info.Size() > limits.MaxFileBytes {
		return fmt.Errorf("hermetic capture file budget exceeded")
	}
	selected[path] = selectedFile{Path: path}
	if len(selected) > limits.MaxPaths {
		return fmt.Errorf("hermetic capture path budget exceeded")
	}
	return nil
}

func hasGitComponent(value string) bool {
	for _, component := range strings.Split(value, "/") {
		if component == ".git" {
			return true
		}
	}
	return false
}
