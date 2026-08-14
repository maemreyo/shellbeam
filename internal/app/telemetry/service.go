package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

const (
	derivationSchemaVersion = 1
	producerVersion         = 1
	capabilityVersion       = 1
	producerID              = "shellbeam.telemetry"
)

type Service struct {
	repository   Repository
	platform     string
	architecture string
	producer     core.Producer
	configDigest string
}

func New(repository Repository) (*Service, error) {
	return newService(repository, runtime.GOOS, runtime.GOARCH)
}

func newService(repository Repository, platform, architecture string) (*Service, error) {
	if repository == nil || !safePlatformValue(platform) || !safePlatformValue(architecture) {
		return nil, fmt.Errorf("invalid telemetry service")
	}
	configDigest, err := telemetryConfigDigest(platform, architecture)
	if err != nil {
		return nil, err
	}
	return &Service{
		repository: repository, platform: platform, architecture: architecture,
		producer:     core.Producer{ProducerID: producerID, ProducerVersion: producerVersion, CapabilityVersion: capabilityVersion},
		configDigest: configDigest,
	}, nil
}

type terminalAuthority struct {
	receipt          receipt.Receipt
	receiptDigest    string
	reservation      operation.Reservation
	snapshot         session.Snapshot
	commandSemantics string
	repositoryID     string
	workspaceID      string
	scope            core.ScopeClass
}

func (s *Service) DeriveTerminal(ctx context.Context, scheduled receipt.Receipt) (core.PerformanceRecord, error) {
	if s == nil || s.repository == nil {
		return core.PerformanceRecord{}, fmt.Errorf("telemetry repository unavailable")
	}
	authority, err := s.loadTerminalAuthority(ctx, scheduled)
	if err != nil {
		return core.PerformanceRecord{}, err
	}
	wallMS := authority.snapshot.UpdatedAt.Sub(authority.reservation.CreatedAt).Milliseconds()
	if wallMS < 0 {
		wallMS = 0
	}
	derivationKey, err := core.DerivationKey(authority.receiptDigest, s.producer, derivationSchemaVersion, s.configDigest)
	if err != nil {
		return core.PerformanceRecord{}, err
	}
	unavailable := core.IntMetric{Quality: core.MetricUnavailable}
	record := core.PerformanceRecord{
		SchemaVersion: core.SchemaVersion, DerivationKey: derivationKey, DerivationSchemaVersion: derivationSchemaVersion,
		DerivationConfigDigest: s.configDigest, Producer: s.producer,
		OperationID: authority.receipt.OperationID, SessionID: authority.receipt.SessionID, ReceiptDigest: authority.receiptDigest,
		RepositoryID: authority.repositoryID, WorkspaceID: authority.workspaceID, ActivityID: authority.reservation.ActivityID,
		CommandSemanticsFingerprint: authority.commandSemantics, ScopeClass: authority.scope,
		Platform: s.platform, Architecture: s.architecture,
		WallMS: wallMS, OutputBytes: authority.receipt.OutputBytes, InputAcceptedBytes: authority.receipt.InputAcceptedBytes, InputDeliveredBytes: authority.receipt.InputDeliveredBytes,
		TerminalOutcome: authority.receipt.Outcome, TimedOut: authority.receipt.Outcome == session.Timeout, CapturedAt: authority.snapshot.UpdatedAt,
		Lifecycle: core.LifecycleTerminal, Completeness: core.CompletenessPartial,
		Resources: core.ResourceMetrics{
			CPUUserMS: unavailable, CPUSystemMS: unavailable, MaxRSSBytes: unavailable,
			ReadBytes: unavailable, WriteBytes: unavailable, ProcessCountPeak: unavailable,
		},
	}
	if err := record.Validate(); err != nil {
		return core.PerformanceRecord{}, err
	}
	if err := s.repository.PutPerformanceRecord(ctx, record); err != nil {
		return core.PerformanceRecord{}, err
	}
	return record, nil
}

func (s *Service) loadTerminalAuthority(ctx context.Context, scheduled receipt.Receipt) (terminalAuthority, error) {
	if err := ctx.Err(); err != nil {
		return terminalAuthority{}, err
	}
	if err := scheduled.Validate(); err != nil || !scheduled.State.Terminal() {
		return terminalAuthority{}, fmt.Errorf("invalid telemetry terminal receipt")
	}
	operationID, err := operation.ParseID(scheduled.OperationID)
	if err != nil {
		return terminalAuthority{}, err
	}
	sessionID, err := operation.ParseSessionID(scheduled.SessionID)
	if err != nil {
		return terminalAuthority{}, err
	}
	stored, err := s.repository.LoadReceipt(ctx, sessionID)
	if err != nil {
		return terminalAuthority{}, err
	}
	if err := stored.Validate(); err != nil || !stored.State.Terminal() || stored.OperationID != string(operationID) || stored.SessionID != string(sessionID) {
		return terminalAuthority{}, fmt.Errorf("durable terminal receipt mismatch")
	}
	scheduledDigest, err := receipt.Digest(scheduled)
	if err != nil {
		return terminalAuthority{}, err
	}
	storedDigest, err := receipt.Digest(stored)
	if err != nil {
		return terminalAuthority{}, err
	}
	if scheduledDigest != storedDigest {
		return terminalAuthority{}, fmt.Errorf("scheduled terminal receipt is not durable authority")
	}
	reservation, err := s.repository.LoadOperation(ctx, operationID)
	if err != nil {
		return terminalAuthority{}, err
	}
	if reservation.OperationID != operationID || reservation.SessionID != sessionID || reservation.CreatedAt.IsZero() {
		return terminalAuthority{}, fmt.Errorf("telemetry reservation mismatch")
	}
	commandSemantics := stored.ExecutionFingerprint
	if commandSemantics == "" {
		commandSemantics = reservation.ExecutionFingerprint
	}
	if stored.ExecutionFingerprint != "" && reservation.ExecutionFingerprint != "" && stored.ExecutionFingerprint != reservation.ExecutionFingerprint {
		return terminalAuthority{}, fmt.Errorf("telemetry execution fingerprint mismatch")
	}
	snapshot, err := s.repository.LoadSession(ctx, sessionID)
	if err != nil {
		return terminalAuthority{}, err
	}
	if snapshot.OperationID != stored.OperationID || snapshot.SessionID != stored.SessionID || !snapshot.State.Terminal() || snapshot.State != stored.State || snapshot.Outcome != stored.Outcome || snapshot.UpdatedAt.IsZero() {
		return terminalAuthority{}, fmt.Errorf("telemetry terminal session mismatch")
	}
	repositoryID, workspaceID := receiptWorkspaceBinding(stored)
	if reservation.WorkspaceID != "" && workspaceID != "" && reservation.WorkspaceID != workspaceID {
		return terminalAuthority{}, fmt.Errorf("telemetry workspace binding mismatch")
	}
	scope, err := executionScope(reservation.ExecutionMode, stored.ExecutionMode)
	if err != nil {
		return terminalAuthority{}, err
	}
	return terminalAuthority{
		receipt: stored, receiptDigest: storedDigest, reservation: reservation, snapshot: snapshot,
		commandSemantics: commandSemantics, repositoryID: repositoryID, workspaceID: workspaceID, scope: scope,
	}, nil
}

func receiptWorkspaceBinding(rec receipt.Receipt) (string, string) {
	if rec.WorkspaceProvenance == nil {
		return "", ""
	}
	var repositoryID, workspaceID string
	switch rec.WorkspaceProvenance.SchemaVersion {
	case 1:
		repositoryID = string(rec.WorkspaceProvenance.RepositoryID)
		workspaceID = string(rec.WorkspaceProvenance.WorkspaceID)
	case 2:
		repositoryID = string(rec.WorkspaceProvenance.Binding.RepositoryID)
		workspaceID = string(rec.WorkspaceProvenance.Binding.WorkspaceID)
	}
	if repositoryID == "" || workspaceID == "" {
		return "", ""
	}
	return repositoryID, workspaceID
}

func executionScope(reservationMode operation.ExecutionMode, receiptMode string) (core.ScopeClass, error) {
	mode := reservationMode
	if mode == "" {
		mode = operation.ExecutionMode(receiptMode)
	}
	switch mode {
	case operation.ExecutionModeShell:
		return core.ScopeShell, nil
	case operation.ExecutionModeArgv:
		return core.ScopeArgv, nil
	default:
		return "", fmt.Errorf("unsupported telemetry execution mode")
	}
}

func telemetryConfigDigest(platform, architecture string) (string, error) {
	encoded, err := json.Marshal(struct {
		SchemaVersion       int    `json:"schema_version"`
		WallClockAuthority  string `json:"wall_clock_authority"`
		ResourceObservation string `json:"resource_observation"`
		Platform            string `json:"platform"`
		Architecture        string `json:"architecture"`
	}{1, "reservation_created_to_terminal_session_updated", "unavailable", platform, architecture})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func safePlatformValue(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
