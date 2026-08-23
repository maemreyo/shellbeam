package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var (
	ErrAddressEscape        = errors.New("workspace_address_escape")
	ErrInvalidAddress       = errors.New("workspace_address_invalid")
	ErrWorkspaceStale       = errors.New("workspace_stale")
	ErrWorkspaceRootMissing = errors.New("workspace_root_missing")
)

type WorkspaceStateError struct {
	Kind        error
	WorkspaceID core.WorkspaceID
	Reason      string
}

func (e *WorkspaceStateError) Error() string {
	if e == nil || e.Kind == nil {
		return "workspace_stale"
	}
	return e.Kind.Error()
}

func (e *WorkspaceStateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Kind
}

func newWorkspaceStateFailure(kind error, workspaceID core.WorkspaceID, reason string) error {
	stateErr := &WorkspaceStateError{Kind: kind, WorkspaceID: workspaceID, Reason: reason}
	code := failure.WorkspaceStale
	if errors.Is(kind, ErrWorkspaceRootMissing) {
		code = failure.WorkspaceRootMissing
	}
	return failure.New(code, map[string]string{
		"workspace_id": string(workspaceID),
		"reason":       reason,
	}, stateErr)
}

// ResolveAdmissionAddress resolves the workspace identity an execution should
// bind before it is durably admitted. Explicit workspace IDs stay strict. A
// cwd-only v2 request first reuses registered identity, then lazily records the
// Git worktree at cwd when one is discoverable. A non-Git cwd remains an
// ordinary unregistered address.
func (s *Service) ResolveAdmissionAddress(ctx context.Context, address core.Address) (core.ResolvedAddress, error) {
	if err := address.Validate(); err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: %v", ErrInvalidAddress, err)
	}
	if address.WorkspaceID != "" {
		return s.ResolveAddress(ctx, address)
	}

	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	if record, ok := mostSpecificWorkspace(workspaces, address.CWD); ok {
		resolved, resolveErr := s.resolveRegisteredAbsoluteCWD(ctx, record, address.CWD)
		if resolveErr == nil {
			return resolved, nil
		}
		if !errors.Is(resolveErr, ErrWorkspaceStale) && !errors.Is(resolveErr, ErrWorkspaceRootMissing) {
			return core.ResolvedAddress{}, resolveErr
		}
	}

	observation, err := s.git.Inspect(ctx, address.CWD)
	if err != nil {
		return core.ResolvedAddress{LogicalCWD: address.CWD, CWD: filepath.Clean(address.CWD)}, nil
	}
	record, err := s.attachObservation(ctx, observation, "")
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	return s.resolveRegisteredAbsoluteCWD(ctx, record, address.CWD)
}

func (s *Service) resolveRegisteredAbsoluteCWD(ctx context.Context, record core.Workspace, cwd string) (core.ResolvedAddress, error) {
	root, err := s.currentWorkspaceRoot(ctx, record)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: workspace root unavailable", ErrInvalidAddress)
	}
	target, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return core.ResolvedAddress{}, fmt.Errorf("%w: cwd unavailable", ErrInvalidAddress)
	}
	if !pathContains(canonicalRoot, target) {
		return core.ResolvedAddress{}, ErrAddressEscape
	}
	logical, err := filepath.Rel(canonicalRoot, target)
	if err != nil {
		return core.ResolvedAddress{}, err
	}
	return core.ResolvedAddress{WorkspaceID: record.ID, LogicalCWD: logical, CWD: filepath.Clean(target)}, nil
}

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
	} else {
		if _, statErr := os.Stat(root); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return "", newWorkspaceStateFailure(ErrWorkspaceRootMissing, record.ID, "root_missing")
			}
			return "", statErr
		}
		observation, inspectErr := s.git.Inspect(ctx, root)
		if inspectErr != nil {
			return "", newWorkspaceStateFailure(ErrWorkspaceStale, record.ID, "gitdir_unresolved")
		}
		if filepath.Clean(observation.GitDir) != filepath.Clean(record.GitDir) {
			return "", newWorkspaceStateFailure(ErrWorkspaceStale, record.ID, "root_mismatch")
		}
		if observation.Root != "" {
			root = observation.Root
		}
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
