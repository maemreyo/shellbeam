package inputtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type MaterializationRepository interface {
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	PutInputTraceRecord(context.Context, core.Record) error
	LoadInputTraceByOperation(context.Context, string) (core.Record, bool, error)
}

type WorkspaceResolver interface {
	ResolveInputTraceWorkspace(context.Context, string) (string, error)
}

type Materializer struct {
	repository MaterializationRepository
	provider   Finalizer
	workspace  WorkspaceResolver
}

func NewMaterializer(repository MaterializationRepository, provider Finalizer, workspace WorkspaceResolver) *Materializer {
	return &Materializer{repository: repository, provider: provider, workspace: workspace}
}

type materializationAuthority struct {
	receipt       receipt.Receipt
	receiptDigest string
	reservation   operation.Reservation
	binding       core.InstrumentationBinding
}

func (m *Materializer) MaterializeTerminal(ctx context.Context, scheduled receipt.Receipt) (core.Record, error) {
	authority, err := m.loadMaterializationAuthority(ctx, scheduled)
	if err != nil {
		return core.Record{}, err
	}
	if existing, ok, err := m.loadExisting(ctx, authority); err != nil {
		return core.Record{}, err
	} else if ok {
		return existing, m.cleanup(ctx, authority.binding)
	}
	record, err := m.deriveRecord(ctx, authority)
	if err != nil {
		return core.Record{}, err
	}
	if err := m.repository.PutInputTraceRecord(ctx, record); err != nil {
		return core.Record{}, err
	}
	if err := m.cleanup(ctx, authority.binding); err != nil {
		return record, err
	}
	return record, nil
}

func (m *Materializer) loadMaterializationAuthority(ctx context.Context, scheduled receipt.Receipt) (materializationAuthority, error) {
	if m == nil || m.repository == nil {
		return materializationAuthority{}, fmt.Errorf("input trace repository unavailable")
	}
	if err := scheduled.Validate(); err != nil || !scheduled.State.Terminal() {
		return materializationAuthority{}, fmt.Errorf("invalid input trace terminal receipt")
	}
	opID, err := operation.ParseID(scheduled.OperationID)
	if err != nil {
		return materializationAuthority{}, err
	}
	sid, err := operation.ParseSessionID(scheduled.SessionID)
	if err != nil {
		return materializationAuthority{}, err
	}
	durable, err := m.repository.LoadReceipt(ctx, sid)
	if err != nil {
		return materializationAuthority{}, err
	}
	if err := durable.Validate(); err != nil || !durable.State.Terminal() {
		return materializationAuthority{}, fmt.Errorf("invalid durable terminal receipt")
	}
	scheduledDigest, err := receipt.Digest(scheduled)
	if err != nil {
		return materializationAuthority{}, err
	}
	durableDigest, err := receipt.Digest(durable)
	if err != nil {
		return materializationAuthority{}, err
	}
	if scheduledDigest != durableDigest {
		return materializationAuthority{}, fmt.Errorf("scheduled receipt is not durable authority")
	}
	reservation, err := m.repository.LoadOperation(ctx, opID)
	if err != nil {
		return materializationAuthority{}, err
	}
	if reservation.OperationID != opID || reservation.SessionID != sid || reservation.Trace == nil {
		return materializationAuthority{}, fmt.Errorf("input trace reservation mismatch")
	}
	binding := *reservation.Trace
	if err := binding.Validate(); err != nil {
		return materializationAuthority{}, err
	}
	return materializationAuthority{receipt: durable, receiptDigest: durableDigest, reservation: reservation, binding: binding}, nil
}

func (m *Materializer) loadExisting(ctx context.Context, authority materializationAuthority) (core.Record, bool, error) {
	existing, ok, err := m.repository.LoadInputTraceByOperation(ctx, authority.receipt.OperationID)
	if err != nil || !ok {
		return existing, ok, err
	}
	if existing.ReceiptDigest != authority.receiptDigest || existing.TraceID != authority.binding.TraceID {
		return core.Record{}, false, fmt.Errorf("input trace durable record conflict")
	}
	return existing, true, nil
}

func (m *Materializer) deriveRecord(ctx context.Context, authority materializationAuthority) (core.Record, error) {
	record := baseTraceRecord(authority.binding, authority.receipt, authority.receiptDigest)
	if m.provider != nil {
		snapshot, finalizeErr := m.provider.Finalize(ctx, authority.binding)
		if finalizeErr == nil && validSnapshot(authority.binding, snapshot) {
			m.applySnapshot(ctx, &record, authority.reservation, snapshot)
		} else if finalizeErr != nil {
			m.applyProviderFailure(&record, finalizeErr)
		}
	}
	key, err := traceDerivationKey(authority.receiptDigest, authority.binding)
	if err != nil {
		return core.Record{}, err
	}
	record.DerivationKey = key
	if err := record.Validate(); err != nil {
		return core.Record{}, err
	}
	return record, nil
}

func (m *Materializer) applyProviderFailure(record *core.Record, err error) {
	code := failure.Public(err).Code
	if code != failure.InputTraceNotFound && code != failure.InputTraceOwnershipLost {
		return
	}
	record.Outcome = core.OutcomePartial
	record.GapReason = core.GapOwnershipLost
	record.Coverage = downgradeCoverageForGap(record.Coverage)
}

func downgradeCoverageForGap(matrix core.CoverageMatrix) core.CoverageMatrix {
	downgrade := func(value core.Coverage) core.Coverage {
		if value == core.CoverageCompleteForOwnedTree {
			return core.CoveragePartial
		}
		return value
	}
	return core.CoverageMatrix{
		FilesystemReads: downgrade(matrix.FilesystemReads), FilesystemMetadataQueries: downgrade(matrix.FilesystemMetadataQueries),
		DirectoryEnumerations: downgrade(matrix.DirectoryEnumerations), FilesystemWrites: downgrade(matrix.FilesystemWrites),
		ExecutedBinaries: downgrade(matrix.ExecutedBinaries), LoadedLibraries: downgrade(matrix.LoadedLibraries),
		EnvironmentNamesObserved: downgrade(matrix.EnvironmentNamesObserved), NetworkAttempts: downgrade(matrix.NetworkAttempts), ChildProcesses: downgrade(matrix.ChildProcesses),
	}
}

func (m *Materializer) applySnapshot(ctx context.Context, record *core.Record, reservation operation.Reservation, snapshot ProviderSnapshot) {
	record.CaptureStart = snapshot.CaptureStart
	record.CaptureEnd = snapshot.CaptureEnd
	record.Coverage = conservativeCoverage(record.Coverage, snapshot.Coverage)
	if snapshot.GapReason == string(core.GapInstrumentationInactive) {
		record.GapReason = core.GapInstrumentationInactive
		record.Coverage = downgradeCoverageForGap(record.Coverage)
	}
	root := ""
	if reservation.WorkspaceID != "" && m.workspace != nil {
		root, _ = m.workspace.ResolveInputTraceWorkspace(ctx, reservation.WorkspaceID)
	}
	resources, summary := NormalizeResources(NormalizationContext{WorkspaceRoot: root, ExecutionCWD: reservation.CWD}, snapshot.Resources)
	record.Resources = resources
	record.Summary = core.Summary{ResourcesReturned: summary.Returned, ResourcesObserved: max(summary.Observed, len(snapshot.Resources))}
	record.Truncated = snapshot.Truncated || summary.Truncated
	record.Outcome = core.OutcomePartial
	if !record.Truncated && snapshot.GapReason == "" && coverageComplete(record.Coverage) {
		record.Outcome = core.OutcomeComplete
	}
}

func (m *Materializer) cleanup(ctx context.Context, binding core.InstrumentationBinding) error {
	if m.provider == nil {
		return nil
	}
	return m.provider.Cleanup(ctx, binding)
}

func baseTraceRecord(binding core.InstrumentationBinding, rec receipt.Receipt, digest string) core.Record {
	return core.Record{SchemaVersion: core.SchemaVersion, TraceID: binding.TraceID, OperationID: rec.OperationID, SessionID: rec.SessionID, ReceiptDigest: digest, Mode: binding.Mode, Provider: binding.Provider, Platform: binding.Platform, InstrumentationFingerprint: binding.InstrumentationFingerprint, InstrumentationEffect: binding.InstrumentationEffect, Authority: core.AuthorityAdvisory, ScopeKind: core.ScopeObservedInput, MayHaveUnobservedDependencies: true, PreExecCoverageEstablished: binding.PreExecCoverageEstablished, Coverage: binding.Coverage, Outcome: core.OutcomeUnavailable, Summary: core.Summary{}}
}
func traceDerivationKey(receiptDigest string, binding core.InstrumentationBinding) (string, error) {
	bd, err := binding.Digest()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Version int    `json:"version"`
		Receipt string `json:"receipt_digest"`
		Binding string `json:"binding_digest"`
	}{1, receiptDigest, bd})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func validSnapshot(binding core.InstrumentationBinding, s ProviderSnapshot) bool {
	validGap := s.GapReason == "" || s.GapReason == string(core.GapInstrumentationInactive)
	return validGap && s.TraceID == binding.TraceID && !s.CaptureStart.IsZero() && !s.CaptureEnd.IsZero() && !s.CaptureEnd.Before(s.CaptureStart) && s.CaptureEnd.Sub(s.CaptureStart) <= core.MaxTraceCaptureDuration && s.Coverage.Validate(binding.PreExecCoverageEstablished) == nil
}
func coverageComplete(m core.CoverageMatrix) bool {
	for _, v := range []core.Coverage{m.FilesystemReads, m.FilesystemMetadataQueries, m.DirectoryEnumerations, m.FilesystemWrites, m.ExecutedBinaries, m.LoadedLibraries, m.EnvironmentNamesObserved, m.NetworkAttempts, m.ChildProcesses} {
		if v != core.CoverageCompleteForOwnedTree {
			return false
		}
	}
	return true
}
func conservativeCoverage(a, b core.CoverageMatrix) core.CoverageMatrix {
	return core.CoverageMatrix{FilesystemReads: minCoverage(a.FilesystemReads, b.FilesystemReads), FilesystemMetadataQueries: minCoverage(a.FilesystemMetadataQueries, b.FilesystemMetadataQueries), DirectoryEnumerations: minCoverage(a.DirectoryEnumerations, b.DirectoryEnumerations), FilesystemWrites: minCoverage(a.FilesystemWrites, b.FilesystemWrites), ExecutedBinaries: minCoverage(a.ExecutedBinaries, b.ExecutedBinaries), LoadedLibraries: minCoverage(a.LoadedLibraries, b.LoadedLibraries), EnvironmentNamesObserved: minCoverage(a.EnvironmentNamesObserved, b.EnvironmentNamesObserved), NetworkAttempts: minCoverage(a.NetworkAttempts, b.NetworkAttempts), ChildProcesses: minCoverage(a.ChildProcesses, b.ChildProcesses)}
}
func minCoverage(a, b core.Coverage) core.Coverage {
	rank := func(v core.Coverage) int {
		switch v {
		case core.CoverageUnsupported:
			return 0
		case core.CoverageUnknown:
			return 1
		case core.CoveragePartial:
			return 2
		case core.CoverageCompleteForOwnedTree:
			return 3
		}
		return 0
	}
	if rank(a) <= rank(b) {
		return a
	}
	return b
}
