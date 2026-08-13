package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var (
	ErrAddressEscape  = errors.New("workspace_address_escape")
	ErrInvalidAddress = errors.New("workspace_address_invalid")
)

func (s *Service) ResolveAddress(ctx context.Context, address core.Address) (core.ResolvedAddress, error) {
	if err := address.Validate(); err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: %v", ErrInvalidAddress, err)
	}
	if address.WorkspaceID == "" {
		return core.ResolvedAddress{LogicalCWD: address.CWD, CWD: filepath.Clean(address.CWD)}, nil
	}
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	record, err := workspaceByID(workspaces, address.WorkspaceID)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	root, err := s.currentWorkspaceRoot(ctx, record)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	logical := address.LogicalCWD()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: workspace root unavailable", ErrInvalidAddress)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(canonicalRoot, logical))
	if err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: cwd unavailable", ErrInvalidAddress)
	}
	if !pathContains(canonicalRoot, target) {
		return core.ResolvedAddress{}, ErrAddressEscape
	}
	return core.ResolvedAddress{WorkspaceID: record.ID, LogicalCWD: logical, CWD: filepath.Clean(target)}, nil
}

func (s *Service) currentWorkspaceRoot(ctx context.Context, record core.Workspace) (string, error) {
	root := record.Root
	if resolved, err := s.git.ResolveWorktreeRoot(ctx, record.GitDir); err == nil {
		root = resolved
	} else if _, statErr := os.Stat(root); statErr != nil {
		return "", fmt.Errorf("%w: registered worktree unavailable", ErrWorkspaceNotFound)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if root != filepath.Clean(record.Root) {
		record.Root = root
		record.LastSeenAt = s.now()
		if err := s.registry.SaveWorkspace(ctx, record); err != nil {
			return "", err
		}
	}
	return root, nil
}

func workspaceByID(records []core.Workspace, id core.WorkspaceID) (core.Workspace, error) {
	for _, record := range records {
		if record.ID == id {
			return record, nil
		}
	}
	return core.Workspace{}, ErrWorkspaceNotFound
}
