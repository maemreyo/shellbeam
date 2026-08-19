package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func validateRecordsForDerivation(records []core.Record, derivation core.Derivation) error {
	for _, record := range records {
		if err := record.Validate(); err != nil || record.Producer != derivation.Producer || !inputRefBelongs(record.SourceRef, derivation.SourceAuthorityRefs) {
			return fmt.Errorf("structured_record_derivation_mismatch")
		}
	}
	return nil
}

func validateStructuredRecordSet(set structuredRecordSet, derivation core.Derivation) error {
	if set.SchemaVersion != core.SchemaVersionV1 && set.SchemaVersion != core.SchemaVersion || set.DerivationKey != derivation.DerivationKey || len(set.Records) < 1 || len(set.Records) > MaxStructuredRecords {
		return fmt.Errorf("invalid_structured_record_set")
	}
	for _, record := range set.Records {
		if record.SchemaVersion != set.SchemaVersion {
			return fmt.Errorf("structured_record_schema_mismatch")
		}
	}
	return validateRecordsForDerivation(set.Records, derivation)
}

func validateStructuredSummary(summary structuredSummary) error {
	if summary.SchemaVersion != 1 || !validStructuredKey(summary.DerivationKey) {
		return fmt.Errorf("invalid_structured_summary")
	}
	counts := []int{summary.RecordCount, summary.Errors, summary.Warnings, summary.Files, summary.TestPassed, summary.TestFailed, summary.TestSkipped, summary.DiagnosticCount, summary.TestCaseCount, summary.TestSuiteCount, summary.ArtifactCount, summary.MechanicalCount, summary.AdvisoryCount}
	for _, count := range counts {
		if count < 0 || count > MaxStructuredRecords {
			return fmt.Errorf("invalid_structured_summary")
		}
	}
	if summary.RecordCount != summary.DiagnosticCount+summary.TestCaseCount+summary.TestSuiteCount+summary.ArtifactCount || summary.RecordCount != summary.MechanicalCount+summary.AdvisoryCount {
		return fmt.Errorf("invalid_structured_summary")
	}
	return nil
}

func inputRefBelongs(ref core.StructuredInputRef, refs []core.StructuredInputRef) bool {
	for _, candidate := range refs {
		if reflect.DeepEqual(ref, candidate) {
			return true
		}
	}
	return false
}

func validateStructuredDerivation(d core.Derivation) error {
	if err := d.Validate(); err != nil {
		return err
	}
	key, err := core.DerivationKeyForInputs(d.SourceAuthorityRefs, d.Producer, d.DerivationSchemaVersion, d.DerivationConfigDigest)
	if err != nil || key != d.DerivationKey {
		return fmt.Errorf("structured_derivation_identity_mismatch")
	}
	return nil
}

func sameDerivationIdentity(a, b core.Derivation) bool {
	return a.DerivationKey == b.DerivationKey && reflect.DeepEqual(a.SourceAuthorityRefs, b.SourceAuthorityRefs) && a.Producer == b.Producer && a.DerivationSchemaVersion == b.DerivationSchemaVersion && a.DerivationConfigDigest == b.DerivationConfigDigest
}

func allowedDerivationTransition(current, next core.Derivation) bool {
	switch current.Lifecycle {
	case core.LifecyclePending:
		return next.Lifecycle == core.LifecycleProcessing && next.ParseOutcome == ""
	case core.LifecycleProcessing:
		return next.Lifecycle == core.LifecycleTerminal
	case core.LifecycleTerminal:
		return next.Lifecycle == core.LifecycleTerminal && current.ParseOutcome == next.ParseOutcome && next.Completeness == core.CompletenessCompacted && current.Completeness != core.CompletenessCompacted && reflect.DeepEqual(current.SemanticsCoverage, next.SemanticsCoverage)
	default:
		return false
	}
}

func structuredTransitionObservable(current, next core.Derivation) bool {
	return current.Lifecycle != core.LifecycleTerminal && next.Lifecycle == core.LifecycleTerminal || current.Completeness != core.CompletenessCompacted && next.Completeness == core.CompletenessCompacted
}

func structuredObservationSubject(d core.Derivation) string {
	return fmt.Sprintf("derivation:%s:terminal:%s:%s", d.DerivationKey, d.ParseOutcome, d.Completeness)
}

func validStructuredKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func (r *Repository) readDerivationUnlocked(key string) (core.Derivation, error) {
	return readStructuredDerivation(r.derivationPath(key))
}

func (r *Repository) structuredRoot() string     { return filepath.Join(r.root, "structured-results") }
func (r *Repository) structuredInputDir() string { return filepath.Join(r.structuredRoot(), "inputs") }
func (r *Repository) structuredDerivationDir() string {
	return filepath.Join(r.structuredRoot(), "derivations")
}
func (r *Repository) structuredRecordDir() string {
	return filepath.Join(r.structuredRoot(), "records")
}
func (r *Repository) structuredSummaryDir() string {
	return filepath.Join(r.structuredRoot(), "summaries")
}
func (r *Repository) structuredOperationDir() string {
	return filepath.Join(r.structuredRoot(), "operations")
}
func (r *Repository) rawOutputRefPath(sessionID string) string {
	return filepath.Join(r.structuredInputDir(), sessionID+".json")
}
func (r *Repository) derivationPath(key string) string {
	return filepath.Join(r.structuredDerivationDir(), key+".json")
}
func (r *Repository) recordPath(key string) string {
	return filepath.Join(r.structuredRecordDir(), key+".json")
}
func (r *Repository) summaryPath(key string) string {
	return filepath.Join(r.structuredSummaryDir(), key+".json")
}
func (r *Repository) structuredOperationPath(operationID string) string {
	return filepath.Join(r.structuredOperationDir(), operationID+".json")
}
func (r *Repository) structuredSubjectPresent(ctx context.Context, subject string) (bool, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 5 || parts[0] != "derivation" || parts[2] != "terminal" || !validStructuredKey(parts[1]) {
		return false, fmt.Errorf("invalid structured observation subject")
	}
	derivation, err := r.GetDerivation(ctx, parts[1])
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return derivation.Lifecycle == core.LifecycleTerminal && string(derivation.ParseOutcome) == parts[3] && string(derivation.Completeness) == parts[4], nil
}

func diagnosticFileIdentity(location source.SourceLocation) string {
	switch location.Kind {
	case source.LocationProviderReported:
		if location.ProviderReported != nil {
			return "path:" + location.ProviderReported.SanitizedLogicalPath
		}
	case source.LocationResolved:
		if location.Resolved != nil {
			return "source:" + location.Resolved.SourceRefID
		}
	}
	return ""
}
