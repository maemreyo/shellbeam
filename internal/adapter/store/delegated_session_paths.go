package store

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) delegatedSessionDir() string { return filepath.Join(r.root, "delegated-sessions") }
func (r *Repository) delegatedBindingDir() string {
	return filepath.Join(r.delegatedSessionDir(), "bindings")
}
func (r *Repository) delegatedProviderRefDir() string {
	return filepath.Join(r.delegatedSessionDir(), "provider-refs")
}
func (r *Repository) delegatedRecoveryDir() string {
	return filepath.Join(r.delegatedSessionDir(), "active")
}
func (r *Repository) delegatedMutationDir() string {
	return filepath.Join(r.delegatedSessionDir(), "mutations")
}
func (r *Repository) delegatedBindingPath(id operation.SessionID) string {
	return filepath.Join(r.delegatedBindingDir(), string(id)+".json")
}
func (r *Repository) delegatedProviderRefPath(id operation.SessionID) string {
	return filepath.Join(r.delegatedProviderRefDir(), string(id)+".json")
}
func (r *Repository) delegatedRecoveryPath(id operation.SessionID) string {
	return filepath.Join(r.delegatedRecoveryDir(), string(id)+".json")
}
func (r *Repository) delegatedSessionMutationDir(id operation.SessionID) string {
	return filepath.Join(r.delegatedMutationDir(), string(id))
}

func delegatedMutationKey(id delegated.MutationIdentity) string {
	logical := string(id.Kind) + "\x00" + id.SessionID + "\x00" + strconv.FormatUint(uint64(id.Epoch), 10) + "\x00" + id.IdempotencyID
	if id.Kind == delegated.MutationWrite {
		logical += "\x00" + strconv.FormatInt(id.Offset, 10)
	}
	sum := sha256.Sum256([]byte(logical))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) delegatedMutationPath(id delegated.MutationIdentity) string {
	return filepath.Join(r.delegatedSessionMutationDir(operation.SessionID(id.SessionID)), delegatedMutationKey(id)+".json")
}
