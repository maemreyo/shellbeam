package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const maxTerminalLaunchRecordBytes = 16 << 10

func (r *Repository) terminalLaunchDir() string {
	return filepath.Join(r.interactiveHandoffDir(), "terminal-launches")
}

func (r *Repository) terminalLaunchPath(handoffID string) string {
	return filepath.Join(r.terminalLaunchDir(), handoffID+".json")
}

func (r *Repository) ensureTerminalLaunchDirDurable() error {
	dir := r.terminalLaunchDir()
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	return r.writer.syncParent("terminal_launch_mkdir", filepath.Dir(dir)).Err
}

func (r *Repository) verifyTerminalLaunchDir() error {
	info, err := os.Lstat(r.terminalLaunchDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe terminal launch directory")
	}
	return nil
}

func (r *Repository) ReserveTerminalLaunch(ctx context.Context, want terminalapp.LaunchRecord) (terminalapp.LaunchRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return terminalapp.LaunchRecord{}, false, err
	}
	if err := want.Validate(); err != nil {
		return terminalapp.LaunchRecord{}, false, err
	}
	if want.State != core.LaunchLaunching {
		return terminalapp.LaunchRecord{}, false, terminalLaunchConflict(want.HandoffID)
	}

	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()

	if current, err := r.readTerminalLaunchLocked(want.HandoffID); err == nil {
		if !sameTerminalLaunchIdentity(current, want) {
			return terminalapp.LaunchRecord{}, false, terminalLaunchConflict(want.HandoffID)
		}
		return current, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return terminalapp.LaunchRecord{}, false, err
	}

	h2, err := r.loadHandoffRecordLocked(want.HandoffID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return terminalapp.LaunchRecord{}, false, terminalLaunchConflict(want.HandoffID)
		}
		return terminalapp.LaunchRecord{}, false, err
	}
	if h2.State.Phase != handoff.PhaseHumanConnecting || h2.State.DesiredOwner != delegated.OwnerHuman ||
		h2.State.AgentIngress != handoff.IngressFenced || h2.State.HumanIngress != handoff.IngressFenced || h2.State.HumanClient != nil {
		return terminalapp.LaunchRecord{}, false, terminalLaunchConflict(want.HandoffID)
	}
	if err := r.ensureTerminalLaunchDirDurable(); err != nil {
		return terminalapp.LaunchRecord{}, false, err
	}

	write := r.writer.Create(r.terminalLaunchPath(want.HandoffID), want)
	if write.Err == nil {
		return want, true, nil
	}
	if current, readErr := r.readTerminalLaunchLocked(want.HandoffID); readErr == nil {
		if !sameTerminalLaunchIdentity(current, want) {
			return terminalapp.LaunchRecord{}, false, terminalLaunchConflict(want.HandoffID)
		}
		return current, false, nil
	}
	return terminalapp.LaunchRecord{}, false, write.Err
}

func (r *Repository) CompleteTerminalLaunch(ctx context.Context, want terminalapp.LaunchRecord) (terminalapp.LaunchRecord, error) {
	if err := ctx.Err(); err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	if err := want.Validate(); err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	if want.State == core.LaunchLaunching {
		return terminalapp.LaunchRecord{}, terminalLaunchConflict(want.HandoffID)
	}

	r.delegatedSessionMu.Lock()
	defer r.delegatedSessionMu.Unlock()
	current, err := r.readTerminalLaunchLocked(want.HandoffID)
	if err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	if !sameTerminalLaunchIdentity(current, want) {
		return terminalapp.LaunchRecord{}, terminalLaunchConflict(want.HandoffID)
	}
	if current == want {
		return current, nil
	}
	if !terminalLaunchTransitionAllowed(current.State, want.State) {
		return terminalapp.LaunchRecord{}, terminalLaunchConflict(want.HandoffID)
	}

	write := r.writer.Replace(r.terminalLaunchPath(want.HandoffID), want)
	if write.Err == nil {
		return want, nil
	}
	if canonical, readErr := r.readTerminalLaunchLocked(want.HandoffID); readErr == nil && canonical == want {
		return canonical, nil
	}
	return terminalapp.LaunchRecord{}, write.Err
}

func (r *Repository) readTerminalLaunchLocked(handoffID string) (terminalapp.LaunchRecord, error) {
	if err := handoff.ValidateHandoffID(handoffID); err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	if err := r.verifyTerminalLaunchDir(); err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	var record terminalapp.LaunchRecord
	if err := readPrivateJSON(r.terminalLaunchPath(handoffID), maxTerminalLaunchRecordBytes, &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return terminalapp.LaunchRecord{}, ErrNotFound
		}
		return terminalapp.LaunchRecord{}, err
	}
	if err := record.Validate(); err != nil {
		return terminalapp.LaunchRecord{}, err
	}
	return record, nil
}

func sameTerminalLaunchIdentity(a, b terminalapp.LaunchRecord) bool {
	return a.SchemaVersion == b.SchemaVersion && a.HandoffID == b.HandoffID &&
		a.Provider.StableKey() == b.Provider.StableKey() &&
		a.AttachTargetFingerprint == b.AttachTargetFingerprint && a.AttemptID == b.AttemptID
}

func terminalLaunchTransitionAllowed(from, to core.LaunchState) bool {
	switch from {
	case core.LaunchLaunching:
		return to == core.LaunchLaunchedAndClientProven || to == core.LaunchFailed || to == core.LaunchOutcomeUnknownState
	case core.LaunchOutcomeUnknownState:
		return to == core.LaunchLaunchedAndClientProven
	default:
		return false
	}
}

func terminalLaunchConflict(handoffID string) error {
	return failure.New(failure.HandoffConflict, map[string]string{"handoff_id": handoffID}, nil)
}
