package store

import (
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) interactiveHandoffDir() string {
	return filepath.Join(r.root, "interactive-handoffs")
}
func (r *Repository) interactiveHandoffRecordDir() string {
	return filepath.Join(r.interactiveHandoffDir(), "records")
}
func (r *Repository) interactiveHandoffTransactionDir() string {
	return filepath.Join(r.interactiveHandoffDir(), "transactions")
}
func (r *Repository) interactiveHandoffControlDir() string {
	return filepath.Join(r.interactiveHandoffDir(), "controls")
}
func (r *Repository) interactiveHandoffControlSessionDir(handoffID string) string {
	return filepath.Join(r.interactiveHandoffControlDir(), handoffID)
}
func (r *Repository) interactiveHandoffRecordPath(id string) string {
	return filepath.Join(r.interactiveHandoffRecordDir(), id+".json")
}
func (r *Repository) interactiveHandoffTransactionPath(id string) string {
	return filepath.Join(r.interactiveHandoffTransactionDir(), id+".json")
}
func (r *Repository) interactiveHandoffControlPath(handoffID, controlID string) string {
	return filepath.Join(r.interactiveHandoffControlSessionDir(handoffID), controlID+".json")
}
func (r *Repository) delegatedProvenanceDir() string {
	return filepath.Join(r.delegatedSessionDir(), "provenance")
}
func (r *Repository) delegatedProvenancePath(id operation.SessionID) string {
	return filepath.Join(r.delegatedProvenanceDir(), string(id)+".json")
}
