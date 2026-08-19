package verification

import (
	"context"
	"fmt"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

const maxQuiescencePersistentPages = 4

type QuiescenceSource struct {
	receipts   app.TerminalReceiptSource
	persistent app.PersistentBindingSource
	lifecycle  app.LifecycleQuiescenceSource
}

func NewQuiescenceSource(receipts app.TerminalReceiptSource, persistentSource app.PersistentBindingSource, lifecycle app.LifecycleQuiescenceSource) *QuiescenceSource {
	return &QuiescenceSource{receipts: receipts, persistent: persistentSource, lifecycle: lifecycle}
}

func (s *QuiescenceSource) Observe(ctx context.Context, operationID, sessionID, workspaceID string) (core.QuiescenceObservation, bool, error) {
	if s == nil || s.receipts == nil || operationID == "" || sessionID == "" {
		return core.QuiescenceObservation{}, false, fmt.Errorf("invalid quiescence source request")
	}
	rec, err := s.receipts.LoadReceipt(ctx, operation.SessionID(sessionID))
	if err != nil {
		return core.QuiescenceObservation{}, false, err
	}
	if rec.OperationID != operationID || rec.SessionID != sessionID {
		return core.QuiescenceObservation{}, false, fmt.Errorf("receipt identity mismatch")
	}
	cleanupIncomplete := rec.ResourceCleanup != nil && rec.ResourceCleanup.Status == receipt.ResourceCleanupIncomplete
	lifecycle, found, err := s.lifecycleObservation(ctx, operationID)
	if err != nil {
		return core.QuiescenceObservation{}, false, err
	}
	if cleanupIncomplete {
		out := core.ReconcileQuiescence(core.QuiescenceInput{Lifecycle: lifecycle, CleanupIncomplete: true})
		return finalizeQuiescence(out, operationID, sessionID), true, nil
	}
	if !found {
		out := core.ReconcileQuiescence(core.QuiescenceInput{})
		return finalizeQuiescence(out, operationID, sessionID), true, nil
	}
	allowed, complete, err := s.allowedPersistentTransfers(ctx, lifecycle, workspaceID)
	if err != nil {
		return core.QuiescenceObservation{}, false, err
	}
	if !complete {
		out := *lifecycle
		out.Status, out.Quality = core.QuiescenceUnknown, core.QuiescenceQualityUnavailable
		return finalizeQuiescence(out, operationID, sessionID), true, nil
	}
	out := core.ReconcileQuiescence(core.QuiescenceInput{Lifecycle: lifecycle, AllowedTransfers: allowed})
	return finalizeQuiescence(out, operationID, sessionID), true, nil
}

func (s *QuiescenceSource) lifecycleObservation(ctx context.Context, operationID string) (*core.QuiescenceObservation, bool, error) {
	if s.lifecycle == nil {
		return nil, false, nil
	}
	obs, found, err := s.lifecycle.QuiescenceForOperation(ctx, operationID)
	if err != nil || !found {
		return nil, found, err
	}
	return &obs, true, nil
}

func (s *QuiescenceSource) allowedPersistentTransfers(ctx context.Context, lifecycle *core.QuiescenceObservation, workspaceID string) ([]core.ResourceRef, bool, error) {
	claimed := claimedPersistentTransfers(lifecycle)
	if len(claimed) == 0 {
		return nil, true, nil
	}
	if s.persistent == nil {
		return nil, false, nil
	}
	want := make(map[string]bool, len(claimed))
	for _, ref := range claimed {
		want[ref.Ref] = true
	}
	allowed := make([]core.ResourceRef, 0, len(claimed))
	var cursor string
	persistentOnly := true
	for pageNo := 0; pageNo < maxQuiescencePersistentPages; pageNo++ {
		page, err := s.persistent.ListPersistentBindings(ctx, persistent.InspectRequest{WorkspaceID: workspaceID, PersistentOnly: &persistentOnly, Limit: persistent.MaxInspectRows, Cursor: cursor})
		if err != nil {
			return nil, false, err
		}
		for _, binding := range page.Bindings {
			if binding.Validate() != nil || !want[binding.SessionID] {
				continue
			}
			if workspaceID != "" && binding.WorkspaceID != workspaceID {
				continue
			}
			allowed = append(allowed, core.ResourceRef{Kind: core.ResourceKindPersistentSession, Ref: binding.SessionID})
			delete(want, binding.SessionID)
		}
		if len(want) == 0 || page.Continuation == "" {
			return allowed, true, nil
		}
		cursor = page.Continuation
	}
	return allowed, false, nil
}

func claimedPersistentTransfers(lifecycle *core.QuiescenceObservation) []core.ResourceRef {
	if lifecycle == nil {
		return nil
	}
	out := make([]core.ResourceRef, 0, len(lifecycle.Transferred))
	for _, ref := range lifecycle.Transferred {
		if ref.Kind == core.ResourceKindPersistentSession {
			out = append(out, ref)
		}
	}
	return out
}

func finalizeQuiescence(out core.QuiescenceObservation, operationID, sessionID string) core.QuiescenceObservation {
	out.OperationID, out.SessionID = operationID, sessionID
	if out.ObservedAt.IsZero() {
		out.ObservedAt = time.Now().UTC()
	}
	return out
}
