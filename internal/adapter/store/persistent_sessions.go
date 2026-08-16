package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

const (
	maxPersistentBindingBytes       = 16 << 10
	maxPersistentNameClaimBytes     = 4 << 10
	maxPersistentBindingScanRecords = 65536
)

var _ app.PersistentSessionStore = (*Repository)(nil)

type persistentNameClaim struct {
	SchemaVersion int       `json:"schema_version"`
	SessionName   string    `json:"session_name"`
	SessionID     string    `json:"session_id"`
	OperationID   string    `json:"operation_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func (c persistentNameClaim) validate() error {
	if c.SchemaVersion != persistent.SchemaVersion || c.CreatedAt.IsZero() {
		return fmt.Errorf("invalid persistent session name claim")
	}
	if err := persistent.ValidateSessionName(c.SessionName); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(c.SessionID); err != nil {
		return err
	}
	if _, err := operation.ParseID(c.OperationID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) initPersistentSessionStore() error {
	for _, path := range []string{r.persistentSessionDir(), r.persistentBindingDir(), r.persistentNameDir(), r.persistentRecoveryDir()} {
		if err := ensurePrivateDir(path); err != nil {
			return fmt.Errorf("persistent sessions: %w", err)
		}
	}
	return nil
}

func (r *Repository) ReservePersistentBinding(ctx context.Context, want persistent.Binding) (persistent.Binding, bool, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := want.Validate(); err != nil || want.Lifecycle != persistent.LifecycleProvisioning {
		if err == nil {
			err = fmt.Errorf("new persistent binding must be provisioning")
		}
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(want, "invalid_binding", err)}
	}
	sessionID, err := operation.ParseSessionID(want.SessionID)
	if err != nil {
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	operationID, err := operation.ParseID(want.OperationID)
	if err != nil {
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}

	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()

	reservation, loadErr := r.LoadOperation(ctx, operationID)
	if loadErr != nil {
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(want, "reservation_missing", loadErr)}
	}
	if err := validatePersistentReservationBinding(reservation, want); err != nil {
		return persistent.Binding{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(want, "reservation_mismatch", err)}
	}
	if want.SessionName != "" {
		if result := r.reservePersistentNameLocked(want); result.Err != nil {
			return persistent.Binding{}, false, result
		}
	}
	if result := r.ensurePersistentRecoveryMarkerLocked(want); result.Err != nil {
		return persistent.Binding{}, false, result
	}

	path := r.persistentBindingPath(sessionID)
	var existing persistent.Binding
	if err := readPrivateJSON(path, maxPersistentBindingBytes, &existing); err == nil {
		if existing.Validate() != nil {
			return existing, false, app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "binding_corrupt", nil)}
		}
		if reflect.DeepEqual(existing, want) {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "binding_conflict", nil)}
	} else if !errors.Is(err, ErrNotFound) {
		return existing, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, want)
	if result.Err == nil {
		return want, true, result
	}
	if errors.Is(result.Err, os.ErrExist) {
		if err := readPrivateJSON(path, maxPersistentBindingBytes, &existing); err != nil {
			return persistent.Binding{}, false, app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		if reflect.DeepEqual(existing, want) {
			return existing, false, app.StoreResult{Durability: app.DurableChange}
		}
		return existing, false, app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "binding_conflict", nil)}
	}
	return persistent.Binding{}, false, result
}

func (r *Repository) AdvancePersistentBinding(ctx context.Context, want persistent.Binding) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := want.Validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(want, "invalid_binding", err)}
	}
	sessionID, err := operation.ParseSessionID(want.SessionID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}

	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()

	existing, err := r.loadPersistentBindingLocked(sessionID)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: persistentStateConflict(want, "binding_missing", err)}
	}
	if !samePersistentBindingIdentity(existing, want) {
		return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "identity_conflict", nil)}
	}
	if reflect.DeepEqual(existing, want) {
		if want.Lifecycle == persistent.LifecycleTerminal || want.Lifecycle == persistent.LifecycleLost {
			if err := r.removePersistentRecoveryMarkerLocked(sessionID); err != nil {
				return app.StoreResult{Durability: app.DurableChange, Err: err}
			}
		}
		return app.StoreResult{Durability: app.DurableChange}
	}
	if !want.UpdatedAt.After(existing.UpdatedAt) || !validPersistentLifecycleTransition(existing.Lifecycle, want.Lifecycle) {
		return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "lifecycle_conflict", nil)}
	}
	if want.Lifecycle == persistent.LifecycleLive {
		if result := r.ensurePersistentRecoveryMarkerLocked(existing); result.Err != nil {
			return result
		}
	}
	result := r.writer.Replace(r.persistentBindingPath(sessionID), want)
	if result.Err != nil {
		return result
	}
	if want.Lifecycle == persistent.LifecycleTerminal || want.Lifecycle == persistent.LifecycleLost {
		if err := r.removePersistentRecoveryMarkerLocked(sessionID); err != nil {
			return app.StoreResult{Durability: app.DurableChange, Err: err}
		}
	}
	return result
}

func (r *Repository) LoadPersistentBinding(ctx context.Context, sessionID operation.SessionID) (persistent.Binding, error) {
	if err := ctx.Err(); err != nil {
		return persistent.Binding{}, err
	}
	if _, err := operation.ParseSessionID(string(sessionID)); err != nil {
		return persistent.Binding{}, err
	}
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	return r.loadPersistentBindingLocked(sessionID)
}

func (r *Repository) FindPersistentBinding(ctx context.Context, sessionID operation.SessionID) (persistent.Binding, bool, error) {
	binding, err := r.LoadPersistentBinding(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return persistent.Binding{}, false, nil
	}
	if err != nil {
		return persistent.Binding{}, false, err
	}
	return binding, true, nil
}

func (r *Repository) FindPersistentBindingByName(ctx context.Context, name string) (persistent.Binding, bool, error) {
	if err := ctx.Err(); err != nil {
		return persistent.Binding{}, false, err
	}
	if err := persistent.ValidateSessionName(name); err != nil {
		return persistent.Binding{}, false, failure.New(failure.InvalidInput, map[string]string{"field": "session_name"}, err)
	}
	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	return r.findPersistentBindingByNameLocked(name)
}

func (r *Repository) ListPersistentBindings(ctx context.Context, request persistent.InspectRequest) (persistent.BindingPage, error) {
	if err := ctx.Err(); err != nil {
		return persistent.BindingPage{}, err
	}
	filter, limit, err := normalizePersistentBindingInspect(request)
	if err != nil {
		return persistent.BindingPage{}, err
	}
	key, err := r.EventCursorKey(ctx)
	if err != nil {
		return persistent.BindingPage{}, err
	}
	codec, err := newPersistentSessionCursorCodec(key)
	if err != nil {
		return persistent.BindingPage{}, err
	}
	var cut, after persistentCursorPosition
	if request.Cursor != "" {
		cut, after, err = codec.Decode(request.Cursor, filter)
		if err != nil {
			return persistent.BindingPage{}, err
		}
	}

	r.persistentSessionMu.Lock()
	defer r.persistentSessionMu.Unlock()
	bindings, err := r.filteredPersistentBindingsLocked(filter)
	if err != nil {
		return persistent.BindingPage{}, err
	}
	if len(bindings) == 0 {
		return persistent.BindingPage{Bindings: []persistent.Binding{}}, nil
	}
	if request.Cursor == "" {
		cut = persistentPosition(bindings[len(bindings)-1])
	}
	eligible := make([]persistent.Binding, 0, len(bindings))
	for _, binding := range bindings {
		position := persistentPosition(binding)
		if comparePersistentPosition(position, cut) > 0 {
			continue
		}
		if request.Cursor != "" && comparePersistentPosition(position, after) <= 0 {
			continue
		}
		eligible = append(eligible, binding)
	}
	if len(eligible) == 0 {
		return persistent.BindingPage{Bindings: []persistent.Binding{}}, nil
	}
	count := limit
	if count > len(eligible) {
		count = len(eligible)
	}
	page := persistent.BindingPage{Bindings: append([]persistent.Binding(nil), eligible[:count]...)}
	if count < len(eligible) {
		continuation, err := codec.Encode(filter, cut, persistentPosition(page.Bindings[len(page.Bindings)-1]))
		if err != nil {
			return persistent.BindingPage{}, err
		}
		page.Continuation = continuation
	}
	return page, nil
}

func (r *Repository) reservePersistentNameLocked(want persistent.Binding) app.StoreResult {
	claim := persistentNameClaim{
		SchemaVersion: persistent.SchemaVersion, SessionName: want.SessionName,
		SessionID: want.SessionID, OperationID: want.OperationID, CreatedAt: want.CreatedAt,
	}
	if err := claim.validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	path := r.persistentNamePath(want.SessionName)
	var existing persistentNameClaim
	if err := readPrivateJSON(path, maxPersistentNameClaimBytes, &existing); err == nil {
		if err := existing.validate(); err != nil {
			return app.StoreResult{Durability: app.DurableChange, Err: err}
		}
		if existing.SessionName != want.SessionName {
			return app.StoreResult{Durability: app.DurableChange, Err: persistentStateConflict(want, "name_hash_collision", nil)}
		}
		if existing.SessionID == want.SessionID && existing.OperationID == want.OperationID {
			return app.StoreResult{Durability: app.DurableChange}
		}
		return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.PersistentSessionNameConflict, map[string]string{"session_id": existing.SessionID, "session_name": want.SessionName, "reason": "already_bound"}, nil)}
	} else if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, claim)
	if result.Err == nil {
		return result
	}
	if errors.Is(result.Err, os.ErrExist) {
		if err := readPrivateJSON(path, maxPersistentNameClaimBytes, &existing); err != nil {
			return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		if existing.validate() == nil && existing.SessionName == want.SessionName && existing.SessionID == want.SessionID && existing.OperationID == want.OperationID {
			return app.StoreResult{Durability: app.DurableChange}
		}
		return app.StoreResult{Durability: app.DurableChange, Err: failure.New(failure.PersistentSessionNameConflict, map[string]string{"session_name": want.SessionName, "reason": "already_bound"}, nil)}
	}
	return result
}

func (r *Repository) loadPersistentBindingLocked(sessionID operation.SessionID) (persistent.Binding, error) {
	var binding persistent.Binding
	if err := readPrivateJSON(r.persistentBindingPath(sessionID), maxPersistentBindingBytes, &binding); err != nil {
		return persistent.Binding{}, err
	}
	if err := binding.Validate(); err != nil {
		return persistent.Binding{}, err
	}
	if binding.SessionID != string(sessionID) {
		return persistent.Binding{}, fmt.Errorf("persistent binding filename identity mismatch")
	}
	return binding, nil
}

func (r *Repository) findPersistentBindingByNameLocked(name string) (persistent.Binding, bool, error) {
	var claim persistentNameClaim
	if err := readPrivateJSON(r.persistentNamePath(name), maxPersistentNameClaimBytes, &claim); errors.Is(err, ErrNotFound) {
		return persistent.Binding{}, false, nil
	} else if err != nil {
		return persistent.Binding{}, false, err
	}
	if err := claim.validate(); err != nil || claim.SessionName != name {
		if err == nil {
			err = fmt.Errorf("persistent name claim mismatch")
		}
		return persistent.Binding{}, false, err
	}
	binding, err := r.loadPersistentBindingLocked(operation.SessionID(claim.SessionID))
	if err != nil {
		return persistent.Binding{}, false, persistentStateConflict(persistent.Binding{SessionID: claim.SessionID, SessionName: name}, "binding_missing", err)
	}
	if binding.SessionName != name || binding.OperationID != claim.OperationID {
		return persistent.Binding{}, false, persistentStateConflict(binding, "name_binding_mismatch", nil)
	}
	return binding, true, nil
}

func (r *Repository) filteredPersistentBindingsLocked(filter persistentBindingFilter) ([]persistent.Binding, error) {
	if filter.SessionName != "" {
		binding, found, err := r.findPersistentBindingByNameLocked(filter.SessionName)
		if err != nil || !found {
			return nil, err
		}
		if persistentBindingMatches(binding, filter) {
			return []persistent.Binding{binding}, nil
		}
		return []persistent.Binding{}, nil
	}
	file, err := os.Open(r.persistentBindingDir())
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(maxPersistentBindingScanRecords + 1)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxPersistentBindingScanRecords {
		return nil, failure.New(failure.PersistentHistoryExhausted, map[string]string{"reason": "binding_scan_limit"}, nil)
	}
	bindings := make([]persistent.Binding, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe persistent binding entry")
		}
		rawID := strings.TrimSuffix(entry.Name(), ".json")
		sessionID, err := operation.ParseSessionID(rawID)
		if err != nil {
			return nil, fmt.Errorf("invalid persistent binding filename")
		}
		binding, err := r.loadPersistentBindingLocked(sessionID)
		if err != nil {
			return nil, err
		}
		if persistentBindingMatches(binding, filter) {
			bindings = append(bindings, binding)
		}
	}
	sort.Slice(bindings, func(i, j int) bool {
		return comparePersistentPosition(persistentPosition(bindings[i]), persistentPosition(bindings[j])) < 0
	})
	return bindings, nil
}

func persistentBindingMatches(binding persistent.Binding, filter persistentBindingFilter) bool {
	return (filter.SessionName == "" || binding.SessionName == filter.SessionName) &&
		(filter.ActivityID == "" || binding.ActivityID == filter.ActivityID) &&
		(filter.WorkspaceID == "" || binding.WorkspaceID == filter.WorkspaceID) &&
		(filter.State == "" || string(binding.Lifecycle) == filter.State)
}

func normalizePersistentBindingInspect(request persistent.InspectRequest) (persistentBindingFilter, int, error) {
	limit := request.Limit
	if limit == 0 {
		limit = persistent.DefaultInspectRows
	}
	if limit < 1 || limit > persistent.MaxInspectRows {
		return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": "max_records", "reason": "limit"}, nil)
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
	if request.State != "" {
		switch persistent.Lifecycle(request.State) {
		case persistent.LifecycleProvisioning, persistent.LifecycleLive, persistent.LifecycleTerminal, persistent.LifecycleLost:
		default:
			return persistentBindingFilter{}, 0, failure.New(failure.InvalidInput, map[string]string{"field": "state", "reason": "invalid_filter"}, nil)
		}
	}
	return persistentBindingFilter{
		SessionName: request.SessionName, ActivityID: request.ActivityID, WorkspaceID: request.WorkspaceID,
		State: request.State, PersistentOnly: request.PersistentOnly,
	}, limit, nil
}

func validPersistentFilterValue(value string) bool {
	if !utf8.ValidString(value) || len(value) > 128 || strings.ContainsAny(value, "/\\") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validatePersistentReservationBinding(reservation operation.Reservation, binding persistent.Binding) error {
	if reservation.SchemaVersion != 4 || !reservation.Persistent || reservation.TTY {
		return fmt.Errorf("operation is not a persistent non-tty reservation")
	}
	if string(reservation.SessionID) != binding.SessionID || string(reservation.OperationID) != binding.OperationID || reservation.SessionName != binding.SessionName || reservation.ActivityID != binding.ActivityID || reservation.WorkspaceID != binding.WorkspaceID {
		return fmt.Errorf("persistent reservation identity mismatch")
	}
	if reservation.CreatedAt.IsZero() || !reservation.CreatedAt.UTC().Equal(binding.CreatedAt.UTC()) {
		return fmt.Errorf("persistent reservation timestamp mismatch")
	}
	return nil
}

func samePersistentBindingIdentity(a, b persistent.Binding) bool {
	return a.SchemaVersion == b.SchemaVersion && a.SessionID == b.SessionID && a.OperationID == b.OperationID &&
		a.ActivityID == b.ActivityID && a.WorkspaceID == b.WorkspaceID && a.SessionName == b.SessionName &&
		a.Persistent == b.Persistent && a.Supervision == b.Supervision && a.Continuity == b.Continuity &&
		a.SupervisorGenerationID == b.SupervisorGenerationID && a.SupervisorEndpointRef == b.SupervisorEndpointRef &&
		a.CreatedAt.UTC().Equal(b.CreatedAt.UTC())
}
