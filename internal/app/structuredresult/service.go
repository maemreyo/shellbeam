package structuredresult

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Begin(ctx context.Context, refs []core.StructuredInputRef, producer core.Producer, schemaVersion int, configDigest string) (core.Derivation, error) {
	if s == nil || s.repository == nil {
		return core.Derivation{}, fmt.Errorf("structured repository unavailable")
	}
	key, err := core.DerivationKeyForInputs(refs, producer, schemaVersion, configDigest)
	if err != nil {
		return core.Derivation{}, err
	}
	derivation := core.Derivation{SchemaVersion: core.SchemaVersion, DerivationKey: key, SourceAuthorityRefs: append([]core.StructuredInputRef(nil), refs...), Producer: producer, DerivationSchemaVersion: schemaVersion, DerivationConfigDigest: configDigest, Lifecycle: core.LifecyclePending, Completeness: core.CompletenessUnavailable}
	if err := s.repository.PutDerivation(ctx, derivation); err != nil {
		return core.Derivation{}, err
	}
	return derivation, nil
}

func (s *Service) MarkProcessing(ctx context.Context, key string) (core.Derivation, error) {
	derivation, err := s.repository.GetDerivation(ctx, key)
	if err != nil {
		return core.Derivation{}, err
	}
	derivation.SchemaVersion = core.SchemaVersion
	derivation.Lifecycle = core.LifecycleProcessing
	derivation.ParseOutcome = ""
	if err := s.repository.PutDerivation(ctx, derivation); err != nil {
		return core.Derivation{}, err
	}
	return derivation, nil
}

type TerminalMetadata struct {
	CompletenessReason core.CompletenessReason
	ObservedEntries    *core.ObservedEntryCounts
	SemanticsCoverage  *core.ProducerSemanticsCoverage
}

func (s *Service) Complete(ctx context.Context, key string, outcome core.ParseOutcome, completeness core.Completeness, records []core.Record) (core.Derivation, error) {
	return s.CompleteWithMetadata(ctx, key, outcome, completeness, records, TerminalMetadata{})
}

func (s *Service) CompleteWithCoverage(ctx context.Context, key string, outcome core.ParseOutcome, completeness core.Completeness, records []core.Record, coverage *core.ProducerSemanticsCoverage) (core.Derivation, error) {
	return s.CompleteWithMetadata(ctx, key, outcome, completeness, records, TerminalMetadata{SemanticsCoverage: coverage})
}

func (s *Service) CompleteWithMetadata(ctx context.Context, key string, outcome core.ParseOutcome, completeness core.Completeness, records []core.Record, metadata TerminalMetadata) (core.Derivation, error) {
	derivation, err := s.repository.GetDerivation(ctx, key)
	if err != nil {
		return core.Derivation{}, err
	}
	if len(records) > 0 {
		if err := s.repository.PutRecords(ctx, key, records); err != nil {
			return core.Derivation{}, err
		}
	}
	derivation.SchemaVersion = core.SchemaVersion
	if metadata.CompletenessReason != "" || metadata.ObservedEntries != nil {
		derivation.SchemaVersion = core.DerivationSchemaVersionV3
	}
	derivation.Lifecycle = core.LifecycleTerminal
	derivation.ParseOutcome = outcome
	derivation.Completeness = completeness
	derivation.CompletenessReason = metadata.CompletenessReason
	derivation.ObservedEntries = cloneObservedEntries(metadata.ObservedEntries)
	derivation.SemanticsCoverage = cloneSemanticsCoverage(metadata.SemanticsCoverage)
	if err := derivation.Validate(); err != nil {
		return core.Derivation{}, err
	}
	if err := s.repository.PutDerivation(ctx, derivation); err != nil {
		return core.Derivation{}, err
	}
	return derivation, nil
}

func cloneObservedEntries(counts *core.ObservedEntryCounts) *core.ObservedEntryCounts {
	if counts == nil {
		return nil
	}
	copy := *counts
	return &copy
}

func cloneSemanticsCoverage(coverage *core.ProducerSemanticsCoverage) *core.ProducerSemanticsCoverage {
	if coverage == nil {
		return nil
	}
	copy := *coverage
	copy.MechanicallyObservable = append([]string(nil), coverage.MechanicallyObservable...)
	copy.Unavailable = append([]string(nil), coverage.Unavailable...)
	return &copy
}

func (s *Service) Compact(ctx context.Context, key string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("structured repository unavailable")
	}
	return s.repository.CompactRecords(ctx, key)
}
