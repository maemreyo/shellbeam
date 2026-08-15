package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

func (r *Repository) initMutationScopeStore() error {
	for _, path := range []string{
		filepath.Join(r.root, "mutation-scopes"),
		filepath.Join(r.root, "mutation-scopes", "identities"),
		filepath.Join(r.root, "mutation-scopes", "mutations"),
		filepath.Join(r.root, "mutation-scopes", "workspaces"),
		filepath.Join(r.root, "mutation-scopes", "activities"),
	} {
		if err := ensurePrivateDir(path); err != nil {
			return fmt.Errorf("mutation scope store: %w", err)
		}
	}
	return nil
}

func (r *Repository) mutationScopeIdentityPath(scopeID string) string {
	return filepath.Join(r.root, "mutation-scopes", "identities", scopeID+".json")
}
func (r *Repository) mutationScopeClaimPath(mutationID string) string {
	return filepath.Join(r.root, "mutation-scopes", "mutations", mutationID+".json")
}
func (r *Repository) mutationScopeWorkspacePath(workspaceID string) string {
	return filepath.Join(r.root, "mutation-scopes", "workspaces", workspaceID+".json")
}
func (r *Repository) mutationScopeActivityPath(activityID string) string {
	return filepath.Join(r.root, "mutation-scopes", "activities", activityID+".json")
}
func (r *Repository) mutationScopePendingPath() string {
	return filepath.Join(r.root, "mutation-scopes", "pending.json")
}

func readMutationScopeJSON(path string, out any) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > maxMutationScopePrivateBytes {
		return fmt.Errorf("unsafe mutation scope state")
	}
	return readStrict(path, out)
}

func (r *Repository) loadMutationScopeIdentityUnlocked(scopeID string) (core.ScopeIdentity, bool, error) {
	var identity core.ScopeIdentity
	err := readMutationScopeJSON(r.mutationScopeIdentityPath(scopeID), &identity)
	if errors.Is(err, ErrNotFound) {
		return identity, false, nil
	}
	if err != nil {
		return identity, false, err
	}
	if identity.ScopeID != scopeID {
		return identity, false, fmt.Errorf("mutation scope identity mismatch")
	}
	if err := identity.Validate(); err != nil {
		return identity, false, err
	}
	return identity, true, nil
}
