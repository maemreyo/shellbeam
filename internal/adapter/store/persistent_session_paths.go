package store

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func validPersistentLifecycleTransition(from, to persistent.Lifecycle) bool {
	switch from {
	case persistent.LifecycleProvisioning:
		return to == persistent.LifecycleLive || to == persistent.LifecycleTerminal || to == persistent.LifecycleLost
	case persistent.LifecycleLive:
		return to == persistent.LifecycleTerminal || to == persistent.LifecycleLost
	default:
		return false
	}
}

func persistentStateConflict(binding persistent.Binding, reason string, cause error) error {
	details := map[string]string{"session_id": binding.SessionID, "session_name": binding.SessionName, "reason": reason}
	return failure.New(failure.SupervisorStateConflict, details, cause)
}

func (r *Repository) persistentSessionDir() string {
	return filepath.Join(r.root, "persistent-sessions")
}

func (r *Repository) persistentBindingDir() string {
	return filepath.Join(r.persistentSessionDir(), "bindings")
}

func (r *Repository) persistentNameDir() string {
	return filepath.Join(r.persistentSessionDir(), "names")
}

func (r *Repository) persistentBindingPath(sessionID operation.SessionID) string {
	return filepath.Join(r.persistentBindingDir(), string(sessionID)+".json")
}

func (r *Repository) persistentNamePath(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(r.persistentNameDir(), hex.EncodeToString(sum[:])+".json")
}
