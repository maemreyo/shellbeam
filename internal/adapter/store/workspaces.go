package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) SaveRepository(_ context.Context, record core.Repository) error {
	if err := record.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.writer.Replace(filepath.Join(r.root, "repositories", string(record.ID)+".json"), record)
	return result.Err
}

func (r *Repository) SaveWorkspace(_ context.Context, record core.Workspace) error {
	if err := record.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.writer.Replace(filepath.Join(r.root, "workspaces", string(record.ID)+".json"), record)
	return result.Err
}

func (r *Repository) ListRepositories(_ context.Context) ([]core.Repository, error) {
	entries, err := registryFiles(filepath.Join(r.root, "repositories"))
	if err != nil {
		return nil, err
	}
	out := make([]core.Repository, 0, len(entries))
	for _, path := range entries {
		var record core.Repository
		if err := readStrict(path, &record); err != nil {
			return nil, err
		}
		if err := record.Validate(); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *Repository) ListWorkspaces(_ context.Context) ([]core.Workspace, error) {
	entries, err := registryFiles(filepath.Join(r.root, "workspaces"))
	if err != nil {
		return nil, err
	}
	out := make([]core.Workspace, 0, len(entries))
	for _, path := range entries {
		var record core.Workspace
		if err := readStrict(path, &record); err != nil {
			return nil, err
		}
		if err := record.Validate(); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *Repository) DeleteWorkspace(_ context.Context, id core.WorkspaceID) error {
	if _, err := core.ParseWorkspaceID(string(id)); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.root, "workspaces", string(id)+".json")
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return r.writer.syncParent("workspace.delete", filepath.Dir(path)).Err
}

func registryFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe registry entry")
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe registry entry")
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}
