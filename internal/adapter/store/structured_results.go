package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const maxStructuredMetadataBytes = 64 << 10

func (r *Repository) initStructuredResultStore() error {
	for _, dir := range []string{r.structuredRoot(), r.structuredInputDir(), r.structuredDerivationDir(), r.structuredRecordDir(), r.structuredSummaryDir(), r.structuredOperationDir(), r.structuredCaptureAuthorityDir()} {
		if err := ensurePrivateDir(dir); err != nil {
			return fmt.Errorf("structured result store: %w", err)
		}
	}
	return nil
}

func (r *Repository) PutRawOutputRef(ctx context.Context, ref core.RawOutputRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	path := r.rawOutputRefPath(ref.SessionID)
	var current core.RawOutputRef
	if err := readPrivateJSON(path, maxStructuredMetadataBytes, &current); err == nil {
		if current == ref {
			return nil
		}
		return fmt.Errorf("raw_output_ref_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.writer.Create(path, ref).Err
}

func (r *Repository) GetRawOutputRef(ctx context.Context, sessionID string) (core.RawOutputRef, error) {
	if err := ctx.Err(); err != nil {
		return core.RawOutputRef{}, err
	}
	if _, err := operation.ParseSessionID(sessionID); err != nil {
		return core.RawOutputRef{}, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	var ref core.RawOutputRef
	if err := readPrivateJSON(r.rawOutputRefPath(sessionID), maxStructuredMetadataBytes, &ref); err != nil {
		return ref, err
	}
	return ref, ref.Validate()
}

func (r *Repository) PutDerivation(ctx context.Context, next core.Derivation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if next.SchemaVersion != core.SchemaVersion {
		return fmt.Errorf("structured_derivation_write_requires_v2")
	}
	if err := validateStructuredDerivation(next); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	path := r.derivationPath(next.DerivationKey)
	current, err := readStructuredDerivation(path)
	if errors.Is(err, ErrNotFound) {
		if next.Lifecycle != core.LifecyclePending {
			return fmt.Errorf("structured_derivation_must_start_pending")
		}
		return r.writer.Create(path, next).Err
	}
	if err != nil {
		return err
	}
	if err := validateStructuredDerivation(current); err != nil {
		return err
	}
	if reflect.DeepEqual(current, next) || sameDerivationReplay(current, next) {
		return nil
	}
	if !sameDerivationIdentity(current, next) || !allowedDerivationTransition(current, next) {
		return fmt.Errorf("structured_derivation_conflict")
	}
	return r.replaceDerivation(ctx, path, next, structuredTransitionObservable(current, next))
}

func (r *Repository) GetDerivation(ctx context.Context, key string) (core.Derivation, error) {
	if err := ctx.Err(); err != nil {
		return core.Derivation{}, err
	}
	if !validStructuredKey(key) {
		return core.Derivation{}, fmt.Errorf("invalid_derivation_key")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	return r.readDerivationUnlocked(key)
}

func (r *Repository) replaceDerivation(ctx context.Context, path string, next core.Derivation, observable bool) error {
	var seq observation.ChangeSeq
	if observable {
		prepared, result := r.prepareStructuredObservation(ctx, next)
		seq = prepared
		if result.Err != nil {
			return result.Err
		}
	}
	result := r.writer.Replace(path, next)
	if seq != 0 {
		r.finishStructuredObservation(seq, path, next, result)
	}
	return result.Err
}

func (r *Repository) prepareStructuredObservation(ctx context.Context, derivation core.Derivation) (observation.ChangeSeq, app.StoreResult) {
	correlation := observation.Correlation{}
	if len(derivation.SourceAuthorityRefs) > 0 {
		correlation = r.correlationForSession("", derivation.SourceAuthorityRefs[0].SessionID())
	}
	request := observation.PrepareRequest{
		Kind: observation.EventStructuredChanged, Correlation: correlation,
		SubjectRef: structuredObservationSubject(derivation), Summary: "structured results changed",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) finishStructuredObservation(seq observation.ChangeSeq, path string, want core.Derivation, result app.StoreResult) {
	switch result.Durability {
	case app.DurableChange:
		r.commitObservationBestEffort(seq)
	case app.NoDurableChange:
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
	case app.AmbiguousChange:
		got, err := readStructuredDerivation(path)
		if err == nil && (reflect.DeepEqual(got, want) || sameDerivationReplay(got, want)) {
			r.commitObservationBestEffort(seq)
		}
	}
}
