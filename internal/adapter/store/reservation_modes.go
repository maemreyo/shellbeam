package store

import (
	"fmt"
	"slices"

	delegatedsession "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func validateDelegatedReservation(v operation.Reservation) error {
	if v.SessionMode != delegatedsession.ModeDelegatedInteractive || v.AuthorityEpoch != 1 || v.Persistent || v.TTY || v.ExperimentID != "" || v.HermeticBoundary != nil {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.RequestFingerprint == "" || v.ExecutionFingerprint == "" || v.Evidence != nil {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.SessionName != "" {
		if err := persistentsession.ValidateSessionName(v.SessionName); err != nil {
			return fmt.Errorf("invalid delegated reservation: %w", err)
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid delegated reservation")
	}
	if v.ProjectCommand != nil {
		if v.Intent != nil || v.ExecutionMode != operation.ExecutionModeArgv || v.Command != "" || v.Shell != "" || v.Executable == "" || len(v.Argv) == 0 || v.Argv[0] == "" {
			return fmt.Errorf("invalid delegated typed reservation")
		}
		if err := v.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid delegated typed reservation: %w", err)
		}
		if !slices.Equal(v.Argv, v.ProjectCommand.ResolvedArgv) || v.CWD != v.ProjectCommand.ResolvedCWD || v.LogicalCWD != v.ProjectCommand.LogicalCWD || v.WorkspaceID == "" {
			return fmt.Errorf("invalid delegated typed reservation")
		}
		return nil
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return fmt.Errorf("invalid delegated reservation: %w", err)
		}
	}
	switch v.ExecutionMode {
	case operation.ExecutionModeShell:
		if v.Command == "" || len(v.Argv) != 0 || v.Shell == "" || v.Executable == "" {
			return fmt.Errorf("invalid delegated reservation")
		}
	case operation.ExecutionModeArgv:
		if len(v.Argv) == 0 || v.Argv[0] == "" || v.Command != "" || v.Shell != "" || v.Executable == "" {
			return fmt.Errorf("invalid delegated reservation")
		}
	default:
		return fmt.Errorf("invalid delegated reservation")
	}
	return nil
}

func validatePersistentReservation(v operation.Reservation) error {
	if err := validatePersistentPolicyReservation(v); err != nil {
		return err
	}
	if !v.Persistent || v.RequestFingerprint == "" || v.ExecutionFingerprint == "" {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.TTY {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.SessionName != "" {
		if err := persistentsession.ValidateSessionName(v.SessionName); err != nil {
			return fmt.Errorf("invalid persistent reservation: %w", err)
		}
	}
	if v.StructuredAdapter != "" && !operation.ValidStructuredAdapterID(v.StructuredAdapter) {
		return fmt.Errorf("invalid persistent reservation")
	}
	if v.ProjectCommand != nil {
		if v.Intent != nil || v.Evidence != nil || v.ExecutionMode != operation.ExecutionModeArgv || v.Command != "" || v.Shell != "" || v.Executable == "" || len(v.Argv) == 0 || v.Argv[0] == "" {
			return fmt.Errorf("invalid persistent typed reservation")
		}
		if err := v.ProjectCommand.Validate(); err != nil {
			return fmt.Errorf("invalid persistent typed reservation: %w", err)
		}
		if !slices.Equal(v.Argv, v.ProjectCommand.ResolvedArgv) || v.CWD != v.ProjectCommand.ResolvedCWD || v.LogicalCWD != v.ProjectCommand.LogicalCWD || v.WorkspaceID == "" {
			return fmt.Errorf("invalid persistent typed reservation")
		}
		return nil
	}
	if v.Intent != nil {
		if err := v.Intent.Validate(); err != nil {
			return fmt.Errorf("invalid persistent reservation: %w", err)
		}
	}
	switch v.ExecutionMode {
	case operation.ExecutionModeShell:
		if v.Command == "" || len(v.Argv) != 0 || v.Shell == "" || v.Executable == "" {
			return fmt.Errorf("invalid persistent reservation")
		}
	case operation.ExecutionModeArgv:
		if len(v.Argv) == 0 || v.Argv[0] == "" || v.Command != "" || v.Shell != "" || v.Executable == "" {
			return fmt.Errorf("invalid persistent reservation")
		}
	default:
		return fmt.Errorf("invalid persistent reservation")
	}
	return nil
}
