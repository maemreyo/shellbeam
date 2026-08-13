package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (r *Repository) AdvanceSession(_ context.Context, v session.Snapshot) app.StoreResult {
	return r.writer.Replace(filepath.Join(r.root, "sessions", v.SessionID, "metadata.json"), v)
}

func (r *Repository) PublishTerminal(_ context.Context, v receipt.Receipt) app.StoreResult {
	if err := v.Validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.terminalMu.Lock()
	defer r.terminalMu.Unlock()
	path := filepath.Join(r.root, "sessions", v.SessionID, "receipt.json")
	var existing receipt.Receipt
	err := readStrict(path, &existing)
	if err == nil {
		if !reflect.DeepEqual(existing, v) {
			return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("terminal_conflict")}
		}
		return r.repairTerminalMetadata(v)
	}
	if !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	result := r.writer.Create(path, v)
	if result.Err != nil {
		if !errors.Is(result.Err, os.ErrExist) {
			return result
		}
		if err = readStrict(path, &existing); err != nil {
			return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
		}
		if !reflect.DeepEqual(existing, v) {
			return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("terminal_conflict")}
		}
	}
	return r.repairTerminalMetadata(v)
}

func (r *Repository) repairTerminalMetadata(v receipt.Receipt) app.StoreResult {
	path := filepath.Join(r.root, "sessions", v.SessionID, "metadata.json")
	var current session.Snapshot
	if err := readStrict(path, &current); err == nil && terminalSnapshotMatches(current, v) {
		return app.StoreResult{Durability: app.DurableChange}
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	want := session.Snapshot{SchemaVersion: 1, OperationID: v.OperationID, SessionID: v.SessionID, DaemonIncarnation: v.DaemonIncarnation, State: v.State, Outcome: v.Outcome, OutputBytes: v.OutputBytes, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	return r.writer.Replace(path, want)
}

func terminalSnapshotMatches(s session.Snapshot, v receipt.Receipt) bool {
	return s.SchemaVersion == 1 && s.OperationID == v.OperationID && s.SessionID == v.SessionID && s.DaemonIncarnation == v.DaemonIncarnation && s.State == v.State && s.Outcome == v.Outcome && s.OutputBytes == v.OutputBytes
}

func (r *Repository) LoadReceipt(_ context.Context, id operation.SessionID) (receipt.Receipt, error) {
	var v receipt.Receipt
	return v, readStrict(filepath.Join(r.root, "sessions", string(id), "receipt.json"), &v)
}
