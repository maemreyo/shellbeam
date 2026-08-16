//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

var privateRecoveryFiles = map[string]struct{}{
	"capability.bin": {}, "metadata.json": {}, "control.sock": {}, "terminal.json": {},
	"output.spool": {}, "output-ack.json": {}, "input-ledger.json": {}, "kill-ledger.json": {},
	"launch.json": {}, "timeout.json": {},
}

func openVerifiedTerminalSpool(layout Layout, capability Capability, sessionID, generationID string) (*Spool, TerminalRecord, error) {
	record, err := LoadTerminalRecord(layout, capability, sessionID, generationID)
	if err != nil {
		return nil, TerminalRecord{}, err
	}
	limit := record.OutputBytes
	if limit < 1 {
		limit = 1
	}
	spool, err := OpenSpool(layout, limit)
	if err != nil {
		return nil, TerminalRecord{}, err
	}
	if spool.Size() != record.OutputBytes {
		_ = spool.Close()
		return nil, TerminalRecord{}, failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": sessionID, "reason": "terminal_extent_mismatch"}, nil)
	}
	return spool, record, nil
}

func cleanupVerifiedPrivateState(ctx context.Context, layout Layout, capability Capability, sessionID, generationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spool, record, err := openVerifiedTerminalSpool(layout, capability, sessionID, generationID)
	if err != nil {
		return err
	}
	ack := spool.Acknowledged()
	_ = spool.Close()
	if ack != record.OutputBytes {
		return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"session_id": sessionID, "reason": "cleanup_before_ack"}, nil)
	}
	entries, err := os.ReadDir(layout.SessionDir)
	if err != nil {
		return privateStateFailure("cleanup_list")
	}
	for _, entry := range entries {
		if _, ok := privateRecoveryFiles[entry.Name()]; !ok {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "unexpected_private_entry"}, nil)
		}
		path := filepath.Join(layout.SessionDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return privateStateFailure("cleanup_stat")
		}
		if info.Mode()&os.ModeSymlink != 0 || !ownedByCurrent(info) {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "unsafe_private_entry"}, nil)
		}
		if entry.Name() != "control.sock" && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "unsafe_private_entry"}, nil)
		}
		if entry.Name() == "control.sock" && !(info.Mode()&os.ModeSocket != 0 || info.Mode().IsRegular()) {
			return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "unsafe_private_entry"}, nil)
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(layout.SessionDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("private supervisor cleanup")
		}
	}
	if err := os.Remove(layout.SessionDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("private supervisor cleanup")
	}
	return nil
}
