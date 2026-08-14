package store

import (
	"context"
	"errors"
	"fmt"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type structuredOperationIndex struct {
	SchemaVersion int    `json:"schema_version"`
	OperationID   string `json:"operation_id"`
	DerivationKey string `json:"derivation_key"`
}

func (r *Repository) BindOperationDerivation(ctx context.Context, operationID operation.ID, derivationKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := operation.ParseID(string(operationID)); err != nil || !validStructuredKey(derivationKey) {
		return fmt.Errorf("invalid structured operation index")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	if _, err := r.readDerivationUnlocked(derivationKey); err != nil {
		return err
	}
	path := r.structuredOperationPath(string(operationID))
	want := structuredOperationIndex{SchemaVersion: 1, OperationID: string(operationID), DerivationKey: derivationKey}
	var current structuredOperationIndex
	if err := readPrivateJSON(path, maxStructuredMetadataBytes, &current); err == nil {
		if current == want {
			return nil
		}
		return fmt.Errorf("structured operation index conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.writer.Create(path, want).Err
}

func (r *Repository) FindOperationDerivation(ctx context.Context, operationID string) (core.Derivation, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Derivation{}, false, err
	}
	if _, err := operation.ParseID(operationID); err != nil {
		return core.Derivation{}, false, err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	var index structuredOperationIndex
	if err := readPrivateJSON(r.structuredOperationPath(operationID), maxStructuredMetadataBytes, &index); errors.Is(err, ErrNotFound) {
		return core.Derivation{}, false, nil
	} else if err != nil {
		return core.Derivation{}, false, err
	}
	if index.SchemaVersion != 1 || index.OperationID != operationID || !validStructuredKey(index.DerivationKey) {
		return core.Derivation{}, false, fmt.Errorf("invalid structured operation index")
	}
	derivation, err := r.readDerivationUnlocked(index.DerivationKey)
	if err != nil {
		return core.Derivation{}, false, err
	}
	return derivation, true, nil
}

func (r *Repository) GetRecordSummary(ctx context.Context, derivationKey string) (structuredapp.RecordSummary, bool, error) {
	if err := ctx.Err(); err != nil {
		return structuredapp.RecordSummary{}, false, err
	}
	if !validStructuredKey(derivationKey) {
		return structuredapp.RecordSummary{}, false, fmt.Errorf("invalid_derivation_key")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	var summary structuredSummary
	if err := readPrivateJSON(r.summaryPath(derivationKey), maxStructuredMetadataBytes, &summary); errors.Is(err, ErrNotFound) {
		return structuredapp.RecordSummary{}, false, nil
	} else if err != nil {
		return structuredapp.RecordSummary{}, false, err
	}
	if err := validateStructuredSummary(summary); err != nil {
		return structuredapp.RecordSummary{}, false, err
	}
	return structuredapp.RecordSummary{RecordsTotal: summary.RecordCount, Errors: summary.Errors, Warnings: summary.Warnings, Files: summary.Files, TestPassed: summary.TestPassed, TestFailed: summary.TestFailed, TestSkipped: summary.TestSkipped, Mechanical: summary.MechanicalCount, Advisory: summary.AdvisoryCount, Compacted: summary.Compacted}, true, nil
}
