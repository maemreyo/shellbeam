package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type inputAuthorityProvenanceStore interface {
	LoadInputAuthorityProvenance(context.Context, operation.SessionID) (string, error)
}

func (s *Service) delegatedTerminalInputAuthorityProvenance(sessionID string) string {
	store, ok := s.store.(inputAuthorityProvenanceStore)
	if !ok {
		return receipt.InputAuthorityAgentOnly
	}
	return resolveTerminalInputAuthorityProvenance(context.Background(), store, operation.SessionID(sessionID))
}

func resolveTerminalInputAuthorityProvenance(ctx context.Context, store inputAuthorityProvenanceStore, sessionID operation.SessionID) string {
	value, err := store.LoadInputAuthorityProvenance(ctx, sessionID)
	if err != nil {
		return receipt.InputAuthorityHumanWriteGranted
	}
	switch value {
	case receipt.InputAuthorityAgentOnly, receipt.InputAuthorityHumanWriteGranted:
		return value
	default:
		// Receipt v5 has no unknown provenance. An unreadable/future value must
		// never strengthen evidence into the stricter agent-only claim.
		return receipt.InputAuthorityHumanWriteGranted
	}
}
