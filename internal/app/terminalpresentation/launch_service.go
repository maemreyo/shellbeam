package terminalpresentation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const LaunchRecordSchemaVersion = 1

type LaunchRecord struct {
	SchemaVersion           int                   `json:"schema_version"`
	HandoffID               string                `json:"handoff_id"`
	Provider                core.TerminalIdentity `json:"provider"`
	AttachTargetFingerprint string                `json:"attach_target_fingerprint"`
	AttemptID               string                `json:"attempt_id"`
	State                   core.LaunchState      `json:"state"`
	FailureCode             failure.Code          `json:"failure_code,omitempty"`
	FailureReason           string                `json:"failure_reason,omitempty"`
}

func (r LaunchRecord) Validate() error {
	if r.SchemaVersion != LaunchRecordSchemaVersion {
		return errors.New("invalid terminal launch record schema")
	}
	if err := handoff.ValidateHandoffID(r.HandoffID); err != nil {
		return err
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	if !validLaunchDigest(r.AttachTargetFingerprint) || !validLaunchDigest(r.AttemptID) {
		return errors.New("invalid terminal launch digest")
	}
	if err := r.State.Validate(); err != nil {
		return err
	}
	switch r.State {
	case core.LaunchLaunching, core.LaunchLaunchedAndClientProven:
		if r.FailureCode != "" || r.FailureReason != "" {
			return errors.New("terminal launch success state carries failure")
		}
	case core.LaunchFailed:
		if !knownLaunchFailureCode(r.FailureCode) || !validResultToken(r.FailureReason) {
			return errors.New("invalid terminal launch failure")
		}
	case core.LaunchOutcomeUnknownState:
		if r.FailureCode != failure.TerminalLaunchUnknown || !validResultToken(r.FailureReason) {
			return errors.New("invalid terminal launch unknown state")
		}
	default:
		return errors.New("not_attempted is represented by absence, not a durable launch record")
	}
	return nil
}

func NewLaunchReservation(handoffID string, identity core.TerminalIdentity, attachArgv []string) (LaunchRecord, error) {
	if err := identity.Validate(); err != nil {
		return LaunchRecord{}, err
	}
	if err := ValidateAttachArgv(attachArgv); err != nil {
		return LaunchRecord{}, err
	}
	if err := handoff.ValidateHandoffID(handoffID); err != nil {
		return LaunchRecord{}, err
	}
	if attachArgv[4] != handoffID {
		return LaunchRecord{}, errors.New("terminal attach target does not match handoff")
	}
	attachFingerprint := digestLaunchArgv(attachArgv)
	attemptID := digestLaunchFields("terminal-launch-attempt-v1", handoffID, identity.StableKey(), attachFingerprint)
	record := LaunchRecord{
		SchemaVersion: LaunchRecordSchemaVersion, HandoffID: handoffID, Provider: identity,
		AttachTargetFingerprint: attachFingerprint, AttemptID: attemptID, State: core.LaunchLaunching,
	}
	if err := record.Validate(); err != nil {
		return LaunchRecord{}, err
	}
	return record, nil
}

type TerminalLaunchStore interface {
	ReserveTerminalLaunch(context.Context, LaunchRecord) (LaunchRecord, bool, error)
	CompleteTerminalLaunch(context.Context, LaunchRecord) (LaunchRecord, error)
}

type LaunchExecutor interface {
	Launch(context.Context, LaunchRequest) (LaunchResult, error)
}

type LaunchService struct {
	store    TerminalLaunchStore
	launcher LaunchExecutor
	prover   handoffapp.ExactClientProver
}

func NewLaunchService(store TerminalLaunchStore, launcher LaunchExecutor, prover handoffapp.ExactClientProver) *LaunchService {
	return &LaunchService{store: store, launcher: launcher, prover: prover}
}

func (s *LaunchService) EnsurePresented(ctx context.Context, handoffID string, resolution core.Resolution, attachArgv []string) (LaunchRecord, error) {
	if err := ctx.Err(); err != nil {
		return LaunchRecord{}, err
	}
	if s == nil || s.store == nil || s.launcher == nil || s.prover == nil {
		return LaunchRecord{}, errors.New("terminal launch service unavailable")
	}
	if resolution.Selected == nil {
		return LaunchRecord{}, failure.New(failure.TerminalLauncherUnavailable, map[string]string{"reason": "no_terminal_selected"}, nil)
	}
	if err := resolution.Selected.Validate(); err != nil {
		return LaunchRecord{}, err
	}
	reservation, err := NewLaunchReservation(handoffID, resolution.Selected.Evidence.Identity, attachArgv)
	if err != nil {
		return LaunchRecord{}, err
	}
	stored, created, err := s.store.ReserveTerminalLaunch(ctx, reservation)
	if err != nil {
		return LaunchRecord{}, failure.Normalize(err)
	}
	if !sameLaunchIdentity(stored, reservation) {
		return LaunchRecord{}, failure.New(failure.HandoffConflict, map[string]string{"handoff_id": handoffID}, nil)
	}
	if !created {
		return s.reconcileStored(ctx, stored)
	}

	request, err := NewLaunchRequest(reservation.Provider, attachArgv)
	if err != nil {
		return LaunchRecord{}, err
	}
	result, launchErr := s.launcher.Launch(ctx, request)
	if result.Attempted {
		return s.finishAttempted(ctx, reservation, result, launchErr)
	}
	return s.finishKnownFailure(ctx, reservation, result, launchErr)
}

func (s *LaunchService) reconcileStored(ctx context.Context, stored LaunchRecord) (LaunchRecord, error) {
	if err := stored.Validate(); err != nil {
		return LaunchRecord{}, err
	}
	switch stored.State {
	case core.LaunchLaunchedAndClientProven:
		return stored, nil
	case core.LaunchFailed:
		return stored, launchFailure(stored)
	case core.LaunchLaunching, core.LaunchOutcomeUnknownState:
		present, err := s.prover.ExactHumanClientPresent(ctx, stored.HandoffID)
		if err == nil && present {
			proven := terminalLaunchProven(stored)
			return s.store.CompleteTerminalLaunch(ctx, proven)
		}
		if stored.State == core.LaunchOutcomeUnknownState {
			return stored, launchFailure(stored)
		}
		reason := "client_not_proven"
		if err != nil {
			reason = "client_proof_unavailable"
		}
		unknown := terminalLaunchUnknown(stored, reason)
		completed, completeErr := s.store.CompleteTerminalLaunch(ctx, unknown)
		if completeErr != nil {
			return LaunchRecord{}, failure.Normalize(completeErr)
		}
		return completed, launchFailure(completed)
	default:
		return LaunchRecord{}, errors.New("invalid stored terminal launch state")
	}
}

func (s *LaunchService) finishAttempted(ctx context.Context, reservation LaunchRecord, result LaunchResult, launchErr error) (LaunchRecord, error) {
	present, proofErr := s.prover.ExactHumanClientPresent(ctx, reservation.HandoffID)
	if proofErr == nil && present {
		proven := terminalLaunchProven(reservation)
		return s.store.CompleteTerminalLaunch(ctx, proven)
	}
	reason := result.Reason
	if !validResultToken(reason) {
		reason = "client_not_proven"
	}
	if proofErr != nil {
		reason = "client_proof_unavailable"
	} else if launchErr != nil && reason == "client_not_proven" {
		reason = "launch_outcome_untrusted"
	}
	unknown := terminalLaunchUnknown(reservation, reason)
	completed, err := s.store.CompleteTerminalLaunch(ctx, unknown)
	if err != nil {
		return LaunchRecord{}, failure.Normalize(err)
	}
	return completed, launchFailure(completed)
}

func (s *LaunchService) finishKnownFailure(ctx context.Context, reservation LaunchRecord, result LaunchResult, launchErr error) (LaunchRecord, error) {
	code := failure.TerminalLaunchFailed
	reason := result.Reason
	if !validResultToken(reason) {
		reason = "launcher_start_failed"
	}
	if launchErr != nil {
		public := failure.Public(launchErr)
		if knownLaunchFailureCode(public.Code) {
			code = public.Code
			if candidate := public.Details["reason"]; validResultToken(candidate) {
				reason = candidate
			}
		}
	}
	failed := reservation
	failed.State = core.LaunchFailed
	failed.FailureCode = code
	failed.FailureReason = reason
	completed, err := s.store.CompleteTerminalLaunch(ctx, failed)
	if err != nil {
		return LaunchRecord{}, failure.Normalize(err)
	}
	return completed, launchFailure(completed)
}

func terminalLaunchProven(record LaunchRecord) LaunchRecord {
	record.State = core.LaunchLaunchedAndClientProven
	record.FailureCode = ""
	record.FailureReason = ""
	return record
}

func terminalLaunchUnknown(record LaunchRecord, reason string) LaunchRecord {
	record.State = core.LaunchOutcomeUnknownState
	record.FailureCode = failure.TerminalLaunchUnknown
	record.FailureReason = reason
	return record
}

func launchFailure(record LaunchRecord) error {
	if record.FailureCode == "" {
		return nil
	}
	return failure.New(record.FailureCode, map[string]string{
		"provider_id": record.Provider.ProviderID,
		"reason":      record.FailureReason,
	}, nil)
}

func knownLaunchFailureCode(code failure.Code) bool {
	switch code {
	case failure.TerminalLauncherUnavailable, failure.TerminalLaunchFailed, failure.TerminalIdentityAmbiguous:
		return true
	default:
		return false
	}
}

func sameLaunchIdentity(a, b LaunchRecord) bool {
	return a.SchemaVersion == b.SchemaVersion && a.HandoffID == b.HandoffID &&
		a.Provider.StableKey() == b.Provider.StableKey() &&
		a.AttachTargetFingerprint == b.AttachTargetFingerprint && a.AttemptID == b.AttemptID
}

func digestLaunchArgv(argv []string) string {
	h := sha256.New()
	for _, value := range argv {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func digestLaunchFields(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validLaunchDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
