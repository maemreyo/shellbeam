package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type experimentAdmissionClaim struct {
	SchemaVersion                  int       `json:"schema_version"`
	LinkID                         string    `json:"link_id"`
	ExperimentID                   string    `json:"experiment_id"`
	OperationID                    string    `json:"operation_id"`
	SessionID                      string    `json:"session_id"`
	WorkspaceID                    string    `json:"workspace_id"`
	SourceGeneration               string    `json:"source_generation"`
	AcceptedRequestFingerprint     string    `json:"accepted_request_fingerprint"`
	AcceptedExecutionFingerprint   string    `json:"accepted_execution_fingerprint"`
	AcceptedObservationFingerprint string    `json:"accepted_observation_binding_fingerprint"`
	AdmittedAt                     time.Time `json:"admitted_at"`
	LinkSemanticFingerprint        string    `json:"link_semantic_fingerprint"`
}

func (r *Repository) ResolveExperimentAdmissionSession(ctx context.Context, experimentID dp.ExperimentID, operationID operation.ID) (operation.SessionID, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if experimentID == "" {
		return "", false, fmt.Errorf("experiment admission identity required")
	}
	if _, err := operation.ParseID(string(operationID)); err != nil {
		return "", false, err
	}

	var claimSession operation.SessionID
	r.decisionProtocolMu.Lock()
	claim, claimErr := r.loadExperimentAdmissionClaimLocked(experimentID)
	r.decisionProtocolMu.Unlock()
	if claimErr == nil {
		if claim.OperationID != string(operationID) {
			return "", false, dp.NewReasonError(dp.ReasonExperimentExecutionLimitReached, "experiment admission already claimed by another operation")
		}
		parsed, err := operation.ParseSessionID(claim.SessionID)
		if err != nil {
			return "", false, fmt.Errorf("corrupt experiment admission session: %w", err)
		}
		claimSession = parsed
	} else if !errors.Is(claimErr, ErrNotFound) {
		return "", false, claimErr
	}

	var captureSession operation.SessionID
	capture, captureErr := r.FindCaptureAuthority(ctx, operationID)
	if captureErr == nil {
		parsed, err := operation.ParseSessionID(capture.Authority.Intent.SessionID)
		if err != nil {
			return "", false, fmt.Errorf("corrupt structured capture session: %w", err)
		}
		captureSession = parsed
	} else if !errors.Is(captureErr, structuredapp.ErrCaptureAuthorityNotFound) {
		return "", false, captureErr
	}

	if claimSession != "" && captureSession != "" && claimSession != captureSession {
		return "", false, fmt.Errorf("experiment admission claim and structured capture session disagree")
	}
	if claimSession != "" {
		return claimSession, true, nil
	}
	if captureSession != "" {
		return captureSession, true, nil
	}
	return "", false, nil
}

// ReserveExperimentOperation preserves the only permitted combined lock order:
// per-operation -> r.admit -> r.decisionProtocolMu. No Decision Protocol path
// may acquire admission locks while holding decisionProtocolMu.
func (r *Repository) ReserveExperimentOperation(ctx context.Context, want operation.Reservation, requested dp.ExperimentExecutionLink) (operation.Reservation, dp.ExperimentExecutionLink, bool, app.StoreResult) {
	unlock := r.lock(want.OperationID)
	r.admit.Lock()
	r.decisionProtocolMu.Lock()
	stored, link, created, result, compensate := r.reserveExperimentOperationLocked(ctx, want, requested)
	r.decisionProtocolMu.Unlock()
	r.admit.Unlock()
	unlock()
	if compensate {
		created = false
		if err := r.finalizeAmbiguousAdmission(stored); err != nil {
			result.Err = errors.Join(result.Err, err)
		}
		if result.Durability == app.NoDurableChange {
			result.Durability = app.DurableChange
		}
	}
	return stored, link, created, result
}

func (r *Repository) reserveExperimentOperationLocked(ctx context.Context, want operation.Reservation, requested dp.ExperimentExecutionLink) (operation.Reservation, dp.ExperimentExecutionLink, bool, app.StoreResult, bool) {
	path := r.operationPath(want.OperationID)
	var existing operation.Reservation
	if err := readStrict(path, &existing); err == nil {
		stored, _, replay := r.replayExistingReservation(ctx, want, existing)
		if replay.Err != nil {
			return stored, dp.ExperimentExecutionLink{}, false, replay, false
		}
		link, repaired, err := r.repairExperimentAdmissionLocked(stored, requested)
		if err != nil {
			return stored, dp.ExperimentExecutionLink{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}, true
		}
		snap, err := r.LoadSession(context.Background(), stored.SessionID)
		if err != nil {
			return stored, link, false, app.StoreResult{Durability: app.DurableChange, Err: err}, true
		}
		if snap.State.Terminal() {
			return stored, link, false, replay, false
		}
		if snap.State != session.Starting {
			return stored, link, false, replay, false
		}
		if repaired {
			return stored, link, true, replay, false
		}
		// An exact link already existed while the durable session never left
		// Starting. The previous daemon crossed admission but runtime ownership is
		// unknown, so fail closed instead of authorizing a possible second spawn.
		return stored, link, false, replay, true
	} else if !errors.Is(err, ErrNotFound) {
		return existing, dp.ExperimentExecutionLink{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}, false
	}

	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return existing, dp.ExperimentExecutionLink{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}, false
	}
	claim, err := r.ensureExperimentAdmissionClaimLocked(want, requested)
	if err != nil {
		return existing, dp.ExperimentExecutionLink{}, false, app.StoreResult{Durability: app.NoDurableChange, Err: err}, false
	}
	frozen := reservationFromExperimentClaim(want, claim)
	stored, created, reserved := r.reserveOperationLocked(ctx, frozen)
	if reserved.Err != nil {
		return stored, dp.ExperimentExecutionLink{}, false, reserved, false
	}
	link, linkCreated, err := r.ensureExperimentExecutionLinkLocked(claim)
	if err != nil {
		return stored, dp.ExperimentExecutionLink{}, false, app.StoreResult{Durability: app.DurableChange, Err: err}, created
	}
	return stored, link, created || linkCreated, reserved, false
}

func reservationFromExperimentClaim(want operation.Reservation, claim experimentAdmissionClaim) operation.Reservation {
	want.ExperimentID = claim.ExperimentID
	want.SessionID = operation.SessionID(claim.SessionID)
	want.WorkspaceID = claim.WorkspaceID
	want.RequestFingerprint = claim.AcceptedRequestFingerprint
	want.ExecutionFingerprint = claim.AcceptedExecutionFingerprint
	want.ObservationBindingFingerprint = claim.AcceptedObservationFingerprint
	want.CreatedAt = claim.AdmittedAt
	return want
}

func (r *Repository) repairExperimentAdmissionLocked(stored operation.Reservation, requested dp.ExperimentExecutionLink) (dp.ExperimentExecutionLink, bool, error) {
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return dp.ExperimentExecutionLink{}, false, err
	}
	claim, err := r.loadExperimentAdmissionClaimLocked(requested.ExperimentID)
	if err != nil {
		return dp.ExperimentExecutionLink{}, false, err
	}
	if claim.OperationID != string(stored.OperationID) || claim.ExperimentID != stored.ExperimentID ||
		claim.AcceptedRequestFingerprint != stored.RequestFingerprint || claim.AcceptedExecutionFingerprint != stored.ExecutionFingerprint ||
		claim.AcceptedObservationFingerprint != stored.ObservationBindingFingerprint {
		return dp.ExperimentExecutionLink{}, false, fmt.Errorf("experiment admission claim conflicts with durable reservation")
	}
	return r.ensureExperimentExecutionLinkLocked(claim)
}

func (r *Repository) ensureExperimentAdmissionClaimLocked(want operation.Reservation, requested dp.ExperimentExecutionLink) (experimentAdmissionClaim, error) {
	if requested.ExperimentID == "" || want.ExperimentID == "" || requested.ExperimentID != dp.ExperimentID(want.ExperimentID) {
		return experimentAdmissionClaim{}, fmt.Errorf("experiment admission identity mismatch")
	}
	state, found, err := r.findExperimentStateLocked(requested.ExperimentID)
	if err != nil {
		return experimentAdmissionClaim{}, err
	}
	if !found || state.seal == nil || state.abort != nil || state.closure != nil {
		return experimentAdmissionClaim{}, dp.NewReasonError(dp.ReasonExperimentNotSealed, "experiment is not an open sealed experiment")
	}
	if len(state.links) > 0 {
		return experimentAdmissionClaim{}, dp.NewReasonError(dp.ReasonExperimentExecutionLimitReached, "experiment already has an execution link")
	}
	episode, _, ok, err := r.findDecisionEpisodeLocked(state.experiment.EpisodeID)
	if err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("experiment episode unavailable")
		}
		return experimentAdmissionClaim{}, err
	}
	if want.WorkspaceID == "" || want.WorkspaceID != episode.WorkspaceID {
		return experimentAdmissionClaim{}, fmt.Errorf("experiment workspace binding mismatch")
	}
	if state.seal.SourceGeneration != episode.Baseline.SourceGeneration {
		return experimentAdmissionClaim{}, fmt.Errorf("experiment source generation mismatch")
	}
	if existing, err := r.loadExperimentAdmissionClaimLocked(requested.ExperimentID); err == nil {
		if experimentClaimMatchesReservation(existing, want, episode.Baseline.SourceGeneration) {
			return existing, nil
		}
		return experimentAdmissionClaim{}, dp.NewReasonError(dp.ReasonExperimentExecutionLimitReached, "experiment admission already claimed")
	} else if !errors.Is(err, ErrNotFound) {
		return experimentAdmissionClaim{}, err
	}
	admittedAt := want.CreatedAt.UTC()
	if admittedAt.IsZero() {
		admittedAt = r.now().UTC()
	}
	claim := experimentAdmissionClaim{
		SchemaVersion: 1, ExperimentID: string(requested.ExperimentID), OperationID: string(want.OperationID), SessionID: string(want.SessionID),
		WorkspaceID: episode.WorkspaceID, SourceGeneration: episode.Baseline.SourceGeneration,
		AcceptedRequestFingerprint: want.RequestFingerprint, AcceptedExecutionFingerprint: want.ExecutionFingerprint,
		AcceptedObservationFingerprint: want.ObservationBindingFingerprint, AdmittedAt: admittedAt,
	}
	claim.LinkID = deterministicLinkID(claim)
	link := claim.executionLink()
	claim.LinkSemanticFingerprint = experimentLinkSemanticFingerprint(link)
	if err := os.MkdirAll(r.decisionProtocolExperimentAdmissionClaimRoot(), 0o700); err != nil {
		return experimentAdmissionClaim{}, err
	}
	res := r.writer.Create(r.decisionProtocolExperimentAdmissionClaimPath(requested.ExperimentID), claim)
	if res.Err != nil {
		if errors.Is(res.Err, os.ErrExist) {
			existing, readErr := r.loadExperimentAdmissionClaimLocked(requested.ExperimentID)
			if readErr == nil && reflect.DeepEqual(existing, claim) {
				return existing, nil
			}
		}
		return experimentAdmissionClaim{}, res.Err
	}
	return claim, nil
}

func experimentClaimMatchesReservation(c experimentAdmissionClaim, want operation.Reservation, sourceGeneration string) bool {
	return c.SchemaVersion == 1 && c.ExperimentID == want.ExperimentID && c.OperationID == string(want.OperationID) &&
		c.WorkspaceID == want.WorkspaceID && c.SourceGeneration == sourceGeneration &&
		c.AcceptedRequestFingerprint == want.RequestFingerprint && c.AcceptedExecutionFingerprint == want.ExecutionFingerprint &&
		c.AcceptedObservationFingerprint == want.ObservationBindingFingerprint
}

func (r *Repository) loadExperimentAdmissionClaimLocked(id dp.ExperimentID) (experimentAdmissionClaim, error) {
	var claim experimentAdmissionClaim
	if err := readStrict(r.decisionProtocolExperimentAdmissionClaimPath(id), &claim); err != nil {
		return claim, err
	}
	if claim.SchemaVersion != 1 || claim.ExperimentID != string(id) || claim.LinkSemanticFingerprint != experimentLinkSemanticFingerprint(claim.executionLink()) {
		return claim, fmt.Errorf("corrupt experiment admission claim")
	}
	return claim, nil
}

func (c experimentAdmissionClaim) executionLink() dp.ExperimentExecutionLink {
	return dp.ExperimentExecutionLink{SchemaVersion: 1, LinkID: dp.LinkID(c.LinkID), ExperimentID: dp.ExperimentID(c.ExperimentID), OperationID: c.OperationID,
		SessionID: c.SessionID, WorkspaceID: c.WorkspaceID, SourceGeneration: c.SourceGeneration,
		AcceptedRequestFingerprint: c.AcceptedRequestFingerprint, AcceptedExecutionFingerprint: c.AcceptedExecutionFingerprint,
		AcceptedObservationBindingFingerprint: c.AcceptedObservationFingerprint, AdmittedAt: c.AdmittedAt}
}

func (r *Repository) ensureExperimentExecutionLinkLocked(claim experimentAdmissionClaim) (dp.ExperimentExecutionLink, bool, error) {
	state, found, err := r.findExperimentStateLocked(dp.ExperimentID(claim.ExperimentID))
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("experiment unavailable while linking")
		}
		return dp.ExperimentExecutionLink{}, false, err
	}
	want := claim.executionLink()
	if err := want.Validate(); err != nil {
		return dp.ExperimentExecutionLink{}, false, err
	}
	if len(state.links) > 1 {
		return dp.ExperimentExecutionLink{}, false, fmt.Errorf("multiple experiment execution links")
	}
	if len(state.links) == 1 {
		if !reflect.DeepEqual(state.links[0], want) {
			return dp.ExperimentExecutionLink{}, false, fmt.Errorf("experiment claim and canonical link disagree")
		}
		return state.links[0], false, nil
	}
	if _, err := r.appendCanonicalRecordLocked(dp.RecordExperimentExecutionLink, want); err != nil {
		return dp.ExperimentExecutionLink{}, false, err
	}
	return want, true, nil
}

func deterministicLinkID(c experimentAdmissionClaim) string {
	b, _ := json.Marshal(struct {
		ExperimentID, OperationID, SessionID, WorkspaceID, SourceGeneration, Request, Execution, Observation string
		AdmittedAt                                                                                           time.Time
	}{
		c.ExperimentID, c.OperationID, c.SessionID, c.WorkspaceID, c.SourceGeneration, c.AcceptedRequestFingerprint, c.AcceptedExecutionFingerprint, c.AcceptedObservationFingerprint, c.AdmittedAt})
	s := sha256.Sum256(b)
	return "link_" + hex.EncodeToString(s[:])
}

func experimentLinkSemanticFingerprint(link dp.ExperimentExecutionLink) string {
	b, _ := json.Marshal(link)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
