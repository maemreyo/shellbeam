package store

import "github.com/maemreyo/shellbeam/internal/core/operation"

func (r *Repository) indexedActivityOperationIDs(activityID string) ([]operation.ID, bool) {
	r.activityOperationMu.RLock()
	defer r.activityOperationMu.RUnlock()
	if !r.activityOperationIndexReady {
		return nil, false
	}
	set := r.activityOperations[activityID]
	ids := make([]operation.ID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	return ids, true
}

func (r *Repository) addActivityOperationLocked(reservation operation.Reservation) {
	if !r.activityOperationIndexReady || reservation.ActivityID == "" {
		return
	}
	set := r.activityOperations[reservation.ActivityID]
	if set == nil {
		set = map[operation.ID]struct{}{}
		r.activityOperations[reservation.ActivityID] = set
	}
	set[reservation.OperationID] = struct{}{}
}

func (r *Repository) removeActivityOperationLocked(reservation operation.Reservation) {
	if !r.activityOperationIndexReady || reservation.ActivityID == "" {
		return
	}
	set := r.activityOperations[reservation.ActivityID]
	delete(set, reservation.OperationID)
	if len(set) == 0 {
		delete(r.activityOperations, reservation.ActivityID)
	}
}

func addActivityOperationToIndex(index map[string]map[operation.ID]struct{}, reservation operation.Reservation) {
	if reservation.ActivityID == "" {
		return
	}
	set := index[reservation.ActivityID]
	if set == nil {
		set = map[operation.ID]struct{}{}
		index[reservation.ActivityID] = set
	}
	set[reservation.OperationID] = struct{}{}
}
