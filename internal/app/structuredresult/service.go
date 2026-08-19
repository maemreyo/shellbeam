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

func (s *Service) Complete(ctx context.Context, key string, outcome core.ParseOutcome, completeness core.Completeness, records []core.Record) (core.Derivation, error) {
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
	derivation.Lifecycle = core.LifecycleTerminal
	derivation.ParseOutcome = outcome
	derivation.Completeness = completeness
	if err := s.repository.PutDerivation(ctx, derivation); err != nil {
		return core.Derivation{}, err
	}
	return derivation, nil
}

func (s *Service) Compact(ctx context.Context, key string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("structured repository unavailable")
	}
	return s.repository.CompactRecords(ctx, key)
}
