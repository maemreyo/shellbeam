package store

import (
	"fmt"
	"path/filepath"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (r *Repository) decisionProtocolRoot() string { return filepath.Join(r.root, "decision_protocol") }
func (r *Repository) decisionProtocolLedgerDir() string {
	return filepath.Join(r.decisionProtocolRoot(), "ledger")
}
func (r *Repository) decisionProtocolRecordDir() string {
	return filepath.Join(r.decisionProtocolLedgerDir(), "records")
}
func (r *Repository) decisionProtocolHighWaterPath() string {
	return filepath.Join(r.decisionProtocolLedgerDir(), "high_water.json")
}
func (r *Repository) decisionProtocolPolicyRoot() string {
	return filepath.Join(r.decisionProtocolRoot(), "policies")
}
func (r *Repository) decisionProtocolActivationRoot() string {
	return filepath.Join(r.decisionProtocolRoot(), "activations")
}
func (r *Repository) decisionProtocolEffectiveRoot() string {
	return filepath.Join(r.decisionProtocolRoot(), "effective")
}
func (r *Repository) decisionProtocolEpisodeIndexRoot() string {
	return filepath.Join(r.decisionProtocolRoot(), "indexes", "episodes")
}

func (r *Repository) decisionProtocolRecordPath(seq decisionprotocol.RecordSeq) string {
	return filepath.Join(r.decisionProtocolRecordDir(), fmt.Sprintf("%020d.json", seq))
}
func (r *Repository) decisionProtocolEpisodeIndexPath(episode decisionprotocol.EpisodeID, seq decisionprotocol.RecordSeq) string {
	return filepath.Join(r.decisionProtocolEpisodeIndexRoot(), string(episode), fmt.Sprintf("%020d.json", seq))
}
func (r *Repository) decisionProtocolPolicyPath(repo, digest string) string {
	return filepath.Join(r.decisionProtocolPolicyRoot(), repo, digest+".json")
}
func (r *Repository) decisionProtocolActivationPath(repo, id string) string {
	return filepath.Join(r.decisionProtocolActivationRoot(), repo, id+".json")
}
func (r *Repository) decisionProtocolEffectivePath(repo string, kind decisionprotocol.EpisodeKind) string {
	return filepath.Join(r.decisionProtocolEffectiveRoot(), repo, string(kind)+".json")
}

// DecisionProtocolStore namespaces Decision Protocol persistence so it can
// expose policy method names that intentionally mirror verification semantics
// without overloading the existing Repository verification methods.
type DecisionProtocolStore struct{ repository *Repository }

func NewDecisionProtocolStore(repository *Repository) *DecisionProtocolStore {
	return &DecisionProtocolStore{repository: repository}
}
