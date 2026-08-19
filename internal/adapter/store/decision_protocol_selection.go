package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func (s *DecisionProtocolStore) RecordSelectionProposal(_ context.Context, proposal dp.SelectionProposal) (dp.CanonicalRecordEnvelope, bool, error) {
	if s == nil || s.repository == nil {
		return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := proposal.Validate(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if err := r.requireDecisionCandidateInEpisodeLocked(proposal.EpisodeID, proposal.CandidateID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	}
	if existing, env, found, err := r.findSelectionProposalLocked(proposal.ProposalID); err != nil {
		return dp.CanonicalRecordEnvelope{}, false, err
	} else if found {
		if !reflect.DeepEqual(existing, proposal) {
			return dp.CanonicalRecordEnvelope{}, false, fmt.Errorf("selection proposal id conflicts with different canonical body")
		}
		return env, false, nil
	}
	env, err := r.appendCanonicalRecordLocked(dp.RecordSelectionProposal, proposal)
	return env, err == nil, err
}

func (s *DecisionProtocolStore) CommitSelectionCAS(_ context.Context, intent dp.SelectionCommitIntent, commit dp.SelectionCommit) (dp.SelectionCommit, bool, error) {
	if s == nil || s.repository == nil {
		return dp.SelectionCommit{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := intent.Validate(); err != nil {
		return dp.SelectionCommit{}, false, err
	}
	if err := commit.Validate(); err != nil {
		return dp.SelectionCommit{}, false, err
	}
	fingerprint, err := dp.SelectionIntentFingerprint(intent)
	if err != nil {
		return dp.SelectionCommit{}, false, err
	}
	if fingerprint != commit.SemanticIntentFingerprint || !commitMatchesIntent(commit, intent) {
		return dp.SelectionCommit{}, false, fmt.Errorf("selection commit does not match semantic intent")
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.SelectionCommit{}, false, err
	}
	if err := r.requireDecisionCandidateInEpisodeLocked(intent.EpisodeID, intent.CandidateID); err != nil {
		return dp.SelectionCommit{}, false, err
	}
	if existing, found, err := r.findSelectionCommitByIdempotencyKeyLocked(commit.IdempotencyKey); err != nil {
		return dp.SelectionCommit{}, false, err
	} else if found {
		if existing.SemanticIntentFingerprint == commit.SemanticIntentFingerprint {
			return existing, false, nil
		}
		return dp.SelectionCommit{}, false, dp.NewReasonError(dp.ReasonIdempotencyConflict, "idempotency key reused for different selection intent")
	}
	terminal, err := r.findDecisionEpisodeTerminalLocked(intent.EpisodeID)
	if err != nil {
		return dp.SelectionCommit{}, false, err
	}
	if terminal.commit != nil {
		return dp.SelectionCommit{}, false, dp.NewReasonError(dp.ReasonTerminalSelectionConflict, "selection already committed")
	}
	if terminal.closure != nil {
		return dp.SelectionCommit{}, false, dp.NewReasonError(dp.ReasonEpisodeTerminalConflict, "episode already closed")
	}
	if _, err := r.appendCanonicalRecordLocked(dp.RecordSelectionCommit, commit); err != nil {
		return dp.SelectionCommit{}, false, err
	}
	return commit, true, nil
}

func (s *DecisionProtocolStore) CloseEpisodeCAS(_ context.Context, closure dp.DecisionClosure) (dp.DecisionClosure, bool, error) {
	if s == nil || s.repository == nil {
		return dp.DecisionClosure{}, false, fmt.Errorf("decision protocol store unavailable")
	}
	if err := closure.Validate(); err != nil {
		return dp.DecisionClosure{}, false, err
	}
	r := s.repository
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.DecisionClosure{}, false, err
	}
	if _, _, found, err := r.findDecisionEpisodeLocked(closure.EpisodeID); err != nil {
		return dp.DecisionClosure{}, false, err
	} else if !found {
		return dp.DecisionClosure{}, false, fmt.Errorf("decision episode unavailable")
	}
	terminal, err := r.findDecisionEpisodeTerminalLocked(closure.EpisodeID)
	if err != nil {
		return dp.DecisionClosure{}, false, err
	}
	if terminal.commit != nil {
		return dp.DecisionClosure{}, false, dp.NewReasonError(dp.ReasonEpisodeTerminalConflict, "episode already committed")
	}
	if terminal.closure != nil {
		if closureSemanticEqual(*terminal.closure, closure) {
			return *terminal.closure, false, nil
		}
		return dp.DecisionClosure{}, false, dp.NewReasonError(dp.ReasonEpisodeTerminalConflict, "episode already closed")
	}
	if _, err := r.appendCanonicalRecordLocked(dp.RecordClosure, closure); err != nil {
		return dp.DecisionClosure{}, false, err
	}
	return closure, true, nil
}

type decisionEpisodeTerminal struct {
	commit  *dp.SelectionCommit
	closure *dp.DecisionClosure
}

func (r *Repository) findDecisionEpisodeTerminalLocked(episodeID dp.EpisodeID) (decisionEpisodeTerminal, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return decisionEpisodeTerminal{}, err
	}
	var out decisionEpisodeTerminal
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		switch env.Kind {
		case dp.RecordSelectionCommit:
			var v dp.SelectionCommit
			if err := json.Unmarshal(env.Body, &v); err != nil {
				return out, err
			}
			if v.EpisodeID == episodeID {
				if out.commit != nil || out.closure != nil {
					return out, fmt.Errorf("duplicate decision episode terminal facts")
				}
				copy := v
				out.commit = &copy
			}
		case dp.RecordClosure:
			var v dp.DecisionClosure
			if err := json.Unmarshal(env.Body, &v); err != nil {
				return out, err
			}
			if v.EpisodeID == episodeID {
				if out.commit != nil || out.closure != nil {
					return out, fmt.Errorf("duplicate decision episode terminal facts")
				}
				copy := v
				out.closure = &copy
			}
		}
	}
	return out, nil
}

func (r *Repository) findSelectionCommitByIdempotencyKeyLocked(key string) (dp.SelectionCommit, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.SelectionCommit{}, false, err
	}
	var found dp.SelectionCommit
	count := 0
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, false, err
		}
		if !ok {
			return found, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != dp.RecordSelectionCommit {
			continue
		}
		var v dp.SelectionCommit
		if err := json.Unmarshal(env.Body, &v); err != nil {
			return found, false, err
		}
		if v.IdempotencyKey == key {
			found = v
			count++
		}
	}
	if count > 1 {
		return found, false, fmt.Errorf("duplicate decision selection idempotency key")
	}
	return found, count == 1, nil
}

func (r *Repository) findSelectionProposalLocked(id string) (dp.SelectionProposal, dp.CanonicalRecordEnvelope, bool, error) {
	hw, err := r.readDecisionProtocolHighWaterLocked()
	if err != nil {
		return dp.SelectionProposal{}, dp.CanonicalRecordEnvelope{}, false, err
	}
	var found dp.SelectionProposal
	var foundEnv dp.CanonicalRecordEnvelope
	count := 0
	for seq := dp.RecordSeq(1); seq <= hw; seq++ {
		env, ok, err := r.loadDecisionProtocolRecordLocked(seq)
		if err != nil {
			return found, foundEnv, false, err
		}
		if !ok {
			return found, foundEnv, false, fmt.Errorf("decision protocol ledger gap at %d", seq)
		}
		if env.Kind != dp.RecordSelectionProposal {
			continue
		}
		var v dp.SelectionProposal
		if err := json.Unmarshal(env.Body, &v); err != nil {
			return found, foundEnv, false, err
		}
		if v.ProposalID == id {
			found, foundEnv, count = v, env, count+1
		}
	}
	if count > 1 {
		return found, foundEnv, false, fmt.Errorf("duplicate selection proposal identity")
	}
	return found, foundEnv, count == 1, nil
}

func (r *Repository) requireDecisionCandidateInEpisodeLocked(episodeID dp.EpisodeID, candidateID dp.CandidateID) error {
	if _, _, found, err := r.findDecisionEpisodeLocked(episodeID); err != nil {
		return err
	} else if !found {
		return fmt.Errorf("decision episode unavailable")
	}
	candidate, _, found, err := r.findDecisionCandidateLocked(candidateID)
	if err != nil {
		return err
	}
	if !found || candidate.EpisodeID != episodeID {
		return fmt.Errorf("decision candidate unavailable in episode")
	}
	return nil
}

func commitMatchesIntent(commit dp.SelectionCommit, intent dp.SelectionCommitIntent) bool {
	return commit.EpisodeID == intent.EpisodeID && commit.CandidateID == intent.CandidateID && commit.CommittedByActorRef == intent.ActorRef && commit.PolicyDigest == intent.PolicyDigest && commit.ProjectionDigest == intent.ProjectionDigest && commit.SourceGeneration == intent.SourceGeneration && ((commit.OverrideRef != "") == intent.Override) && commit.OverrideRef == intent.OverrideRef
}

func closureSemanticEqual(a, b dp.DecisionClosure) bool {
	return a.EpisodeID == b.EpisodeID && a.Kind == b.Kind && a.Reason == b.Reason && reflect.DeepEqual(a.UnresolvedDimensions, b.UnresolvedDimensions) && a.ActorRef == b.ActorRef && a.ProjectionDigest == b.ProjectionDigest
}
