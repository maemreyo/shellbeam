package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) ListSessionSummaries(ctx context.Context, request persistent.InspectRequest) (persistent.InspectPage, error) {
	if err := ctx.Err(); err != nil {
		return persistent.InspectPage{}, err
	}
	filter, limit, err := normalizeSessionSummaryInspect(request)
	if err != nil {
		return persistent.InspectPage{}, err
	}
	key, err := r.EventCursorKey(ctx)
	if err != nil {
		return persistent.InspectPage{}, err
	}
	codec, err := newPersistentSessionCursorCodec(key)
	if err != nil {
		return persistent.InspectPage{}, err
	}
	var cut, after persistentCursorPosition
	if request.Cursor != "" {
		cut, after, err = codec.Decode(request.Cursor, filter)
		if err != nil {
			return persistent.InspectPage{}, err
		}
	}

	summaries, err := r.filteredSessionSummaries(ctx, filter)
	if err != nil {
		return persistent.InspectPage{}, err
	}
	if len(summaries) == 0 {
		return persistent.InspectPage{Sessions: []persistent.Summary{}}, nil
	}
	if request.Cursor == "" {
		last := summaries[len(summaries)-1]
		cut = persistentCursorPosition{CreatedAt: last.CreatedAt.UTC(), SessionID: last.SessionID}
	}
	eligible := make([]persistent.Summary, 0, len(summaries))
	for _, summary := range summaries {
		position := persistentCursorPosition{CreatedAt: summary.CreatedAt.UTC(), SessionID: summary.SessionID}
		if comparePersistentPosition(position, cut) > 0 {
			continue
		}
		if request.Cursor != "" && comparePersistentPosition(position, after) <= 0 {
			continue
		}
		eligible = append(eligible, summary)
	}
	if len(eligible) == 0 {
		return persistent.InspectPage{Sessions: []persistent.Summary{}}, nil
	}
	count := limit
	if count > len(eligible) {
		count = len(eligible)
	}
	page := persistent.InspectPage{Sessions: append([]persistent.Summary(nil), eligible[:count]...)}
	if count < len(eligible) {
		last := page.Sessions[len(page.Sessions)-1]
		continuation, err := codec.Encode(filter, cut, persistentCursorPosition{CreatedAt: last.CreatedAt.UTC(), SessionID: last.SessionID})
		if err != nil {
			return persistent.InspectPage{}, err
		}
		page.Continuation = continuation
	}
	return page, nil
}

func (r *Repository) filteredSessionSummaries(ctx context.Context, filter persistentBindingFilter) ([]persistent.Summary, error) {
	if filter.ActivityID != "" {
		if ids, ready := r.indexedActivityOperationIDs(filter.ActivityID); ready {
			return r.filteredSessionSummariesFromOperationIDs(ctx, filter, ids)
		}
	}
	return r.filteredSessionSummariesFromOperationScan(ctx, filter)
}

func (r *Repository) filteredSessionSummariesFromOperationIDs(ctx context.Context, filter persistentBindingFilter, ids []operation.ID) ([]persistent.Summary, error) {
	out := make([]persistent.Summary, 0, len(ids))
	for _, id := range ids {
		reservation, err := r.LoadOperation(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !sessionReservationMatchesFilter(reservation, filter) {
			continue
		}
		snapshot, err := r.LoadSession(ctx, reservation.SessionID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if filter.State != "" && string(snapshot.State) != filter.State {
			continue
		}
		summary, err := r.canonicalSessionSummary(ctx, reservation, snapshot)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	sortSessionSummaries(out)
	return out, nil
}

func (r *Repository) filteredSessionSummariesFromOperationScan(ctx context.Context, filter persistentBindingFilter) ([]persistent.Summary, error) {
	file, err := os.Open(filepath.Join(r.root, "operations"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(maxPersistentBindingScanRecords + 1)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxPersistentBindingScanRecords {
		return nil, failure.New(failure.PersistentHistoryExhausted, map[string]string{"reason": "session_scan_limit"}, nil)
	}
	out := make([]persistent.Summary, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe operation entry")
		}
		id, err := operation.ParseID(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, fmt.Errorf("invalid operation filename")
		}
		reservation, err := r.LoadOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		if !sessionReservationMatchesFilter(reservation, filter) {
			continue
		}
		snapshot, err := r.LoadSession(ctx, reservation.SessionID)
		if err != nil {
			return nil, err
		}
		if filter.State != "" && string(snapshot.State) != filter.State {
			continue
		}
		summary, err := r.canonicalSessionSummary(ctx, reservation, snapshot)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	sortSessionSummaries(out)
	return out, nil
}

func sessionReservationMatchesFilter(reservation operation.Reservation, filter persistentBindingFilter) bool {
	if filter.PersistentOnly && !reservation.Persistent {
		return false
	}
	if filter.SessionName != "" && reservation.SessionName != filter.SessionName {
		return false
	}
	if filter.ActivityID != "" && reservation.ActivityID != filter.ActivityID {
		return false
	}
	if filter.WorkspaceID != "" && reservation.WorkspaceID != filter.WorkspaceID {
		return false
	}
	return true
}

func sortSessionSummaries(summaries []persistent.Summary) {
	sort.Slice(summaries, func(i, j int) bool {
		a := persistentCursorPosition{CreatedAt: summaries[i].CreatedAt.UTC(), SessionID: summaries[i].SessionID}
		b := persistentCursorPosition{CreatedAt: summaries[j].CreatedAt.UTC(), SessionID: summaries[j].SessionID}
		return comparePersistentPosition(a, b) < 0
	})
}

func (r *Repository) canonicalSessionSummary(ctx context.Context, reservation operation.Reservation, snapshot session.Snapshot) (persistent.Summary, error) {
	summary := persistent.Summary{
		SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID,
		State: string(snapshot.State), Outcome: string(snapshot.Outcome), Persistent: reservation.Persistent,
		CreatedAt: reservation.CreatedAt.UTC(), UpdatedAt: snapshot.UpdatedAt.UTC(), OutputBytes: snapshot.OutputBytes,
		OwnershipStatus: persistent.OwnershipLost,
	}
	if reservation.Persistent {
		summary.SessionName = reservation.SessionName
		binding, err := r.LoadPersistentBinding(ctx, reservation.SessionID)
		if errors.Is(err, ErrNotFound) && snapshot.State.Terminal() {
			summary.OwnershipStatus = persistent.OwnershipTerminal
		} else if err != nil {
			return persistent.Summary{}, err
		} else {
			switch binding.Lifecycle {
			case persistent.LifecycleLost:
				summary.OwnershipStatus = persistent.OwnershipLost
			case persistent.LifecycleTerminal:
				summary.OwnershipStatus = persistent.OwnershipTerminal
			case persistent.LifecycleProvisioning, persistent.LifecycleLive:
				summary.OwnershipStatus = persistent.OwnershipLost
			default:
				return persistent.Summary{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": summary.SessionID, "reason": "inspect_lifecycle"}, nil)
			}
		}
	} else if snapshot.State.Terminal() {
		summary.OwnershipStatus = persistent.OwnershipTerminal
	}
	if snapshot.State.Terminal() {
		rec, err := r.LoadReceipt(ctx, reservation.SessionID)
		if err == nil {
			summary.Outcome = string(rec.Outcome)
			summary.OutputBytes = rec.OutputBytes
			summary.InputAcceptedBytes = rec.InputAcceptedBytes
			summary.InputDeliveredBytes = rec.InputDeliveredBytes
		} else if !errors.Is(err, ErrNotFound) {
			return persistent.Summary{}, err
		}
	}
	return summary, nil
}

func normalizeSessionSummaryInspect(request persistent.InspectRequest) (persistentBindingFilter, int, error) {
	limit := request.Limit
	if limit == 0 {
		limit = persistent.DefaultInspectRows
	}
	if limit < 1 || limit > persistent.MaxInspectRows {
		return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": "max_records", "reason": "limit"}, nil)
	}
	persistentOnly := true
	if request.PersistentOnly != nil {
		persistentOnly = *request.PersistentOnly
	}
	if request.SessionName != "" {
		if err := persistent.ValidateSessionName(request.SessionName); err != nil {
			return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, err)
		}
	}
	for field, value := range map[string]string{"activity_id": request.ActivityID, "workspace_id": request.WorkspaceID} {
		if value != "" && !validPersistentFilterValue(value) {
			return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": field, "reason": "invalid_filter"}, nil)
		}
	}
	if request.State != "" && !validInspectableSessionState(session.State(request.State)) {
		return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": "state", "reason": "invalid_filter"}, nil)
	}
	return persistentBindingFilter{
		SessionName: request.SessionName, ActivityID: request.ActivityID, WorkspaceID: request.WorkspaceID,
		State: request.State, PersistentOnly: persistentOnly,
	}, limit, nil
}

func validInspectableSessionState(state session.State) bool {
	switch state {
	case session.Starting, session.Running, session.Finalizing, session.Completed, session.Failed, session.TimedOut, session.Killed, session.Abandoned:
		return true
	default:
		return false
	}
}
