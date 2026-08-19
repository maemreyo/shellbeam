package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	MaxStructuredRecords         = 1024
	maxStructuredRecordFileBytes = 8 << 20
)

var ErrStructuredRecordsCompacted = errors.New("structured records compacted")

type structuredRecordSet struct {
	SchemaVersion int           `json:"schema_version"`
	DerivationKey string        `json:"derivation_key"`
	Records       []core.Record `json:"records"`
}

type structuredSummary struct {
	SchemaVersion   int    `json:"schema_version"`
	DerivationKey   string `json:"derivation_key"`
	RecordCount     int    `json:"record_count"`
	Errors          int    `json:"errors,omitempty"`
	Warnings        int    `json:"warnings,omitempty"`
	Files           int    `json:"files,omitempty"`
	TestPassed      int    `json:"test_passed,omitempty"`
	TestFailed      int    `json:"test_failed,omitempty"`
	TestSkipped     int    `json:"test_skipped,omitempty"`
	DiagnosticCount int    `json:"diagnostic_count"`
	TestCaseCount   int    `json:"test_case_count"`
	TestSuiteCount  int    `json:"test_suite_count"`
	ArtifactCount   int    `json:"artifact_count"`
	MechanicalCount int    `json:"mechanical_count"`
	AdvisoryCount   int    `json:"advisory_count"`
	Compacted       bool   `json:"compacted"`
}

func (r *Repository) PutRecords(ctx context.Context, key string, records []core.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validStructuredKey(key) || len(records) < 1 || len(records) > MaxStructuredRecords {
		return fmt.Errorf("invalid_structured_records")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	derivation, err := r.readDerivationUnlocked(key)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.SchemaVersion != core.SchemaVersion {
			return fmt.Errorf("structured_record_write_requires_v2")
		}
	}
	if err := validateRecordsForDerivation(records, derivation); err != nil {
		return err
	}
	set := structuredRecordSet{SchemaVersion: core.SchemaVersion, DerivationKey: key, Records: append([]core.Record(nil), records...)}
	path := r.recordPath(key)
	if current, err := readStructuredRecordSet(path, derivation); err == nil {
		if err := validateStructuredRecordSet(current, derivation); err != nil {
			return err
		}
		if !sameRecordSetReplay(current, set) {
			return fmt.Errorf("structured_records_conflict")
		}
		return r.ensureStructuredSummaryUnlocked(set)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if derivation.Lifecycle != core.LifecycleProcessing {
		return fmt.Errorf("structured_records_require_processing")
	}
	if result := r.writer.Create(path, set); result.Err != nil {
		return result.Err
	}
	return r.ensureStructuredSummaryUnlocked(set)
}

func (r *Repository) ListRecords(ctx context.Context, key string, query structuredapp.RecordQuery) ([]core.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validStructuredKey(key) || query.Validate() != nil {
		return nil, fmt.Errorf("invalid_structured_record_query")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	derivation, err := r.readDerivationUnlocked(key)
	if err != nil {
		return nil, err
	}
	if derivation.Completeness == core.CompletenessCompacted {
		return nil, ErrStructuredRecordsCompacted
	}
	set, err := readStructuredRecordSet(r.recordPath(key), derivation)
	if errors.Is(err, ErrNotFound) {
		return []core.Record{}, nil
	} else if err != nil {
		return nil, err
	}
	if query.Offset > len(set.Records) {
		return nil, fmt.Errorf("invalid_structured_record_query")
	}
	end := min(query.Offset+query.Limit, len(set.Records))
	return append([]core.Record(nil), set.Records[query.Offset:end]...), nil
}

func (r *Repository) CompactRecords(ctx context.Context, key string) error {
	derivation, err := r.GetDerivation(ctx, key)
	if err != nil {
		return err
	}
	if derivation.Lifecycle != core.LifecycleTerminal {
		return fmt.Errorf("structured_compaction_requires_terminal")
	}
	if derivation.Completeness == core.CompletenessCompacted {
		return nil
	}
	if err := r.markStructuredSummaryCompacted(ctx, key); err != nil {
		return err
	}
	derivation.SchemaVersion = core.SchemaVersion
	derivation.Completeness = core.CompletenessCompacted
	if err := r.PutDerivation(ctx, derivation); err != nil {
		return err
	}
	if err := os.Remove(r.recordPath(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Repository) readStructuredSummary(key string) (structuredSummary, error) {
	if !validStructuredKey(key) {
		return structuredSummary{}, fmt.Errorf("invalid_derivation_key")
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	var summary structuredSummary
	if err := readPrivateJSON(r.summaryPath(key), maxStructuredMetadataBytes, &summary); err != nil {
		return summary, err
	}
	return summary, validateStructuredSummary(summary)
}

func (r *Repository) markStructuredSummaryCompacted(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	var summary structuredSummary
	err := readPrivateJSON(r.summaryPath(key), maxStructuredMetadataBytes, &summary)
	if errors.Is(err, ErrNotFound) {
		summary = structuredSummary{SchemaVersion: 1, DerivationKey: key, Compacted: true}
		return r.writer.Create(r.summaryPath(key), summary).Err
	}
	if err != nil {
		return err
	}
	if err := validateStructuredSummary(summary); err != nil {
		return err
	}
	if summary.Compacted {
		return nil
	}
	summary.Compacted = true
	return r.writer.Replace(r.summaryPath(key), summary).Err
}

func (r *Repository) ensureStructuredSummaryUnlocked(set structuredRecordSet) error {
	want := summarizeStructuredRecords(set)
	var current structuredSummary
	if err := readPrivateJSON(r.summaryPath(set.DerivationKey), maxStructuredMetadataBytes, &current); err == nil {
		if err := validateStructuredSummary(current); err != nil {
			return err
		}
		if reflect.DeepEqual(current, want) {
			return nil
		}
		return fmt.Errorf("structured_summary_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return r.writer.Create(r.summaryPath(set.DerivationKey), want).Err
}

func summarizeStructuredRecords(set structuredRecordSet) structuredSummary {
	s := structuredSummary{SchemaVersion: 1, DerivationKey: set.DerivationKey, RecordCount: len(set.Records)}
	files := make(map[string]struct{})
	for _, record := range set.Records {
		switch record.RecordKind {
		case core.RecordDiagnostic:
			s.DiagnosticCount++
			if record.Diagnostic != nil {
				switch record.Diagnostic.Severity {
				case core.SeverityError:
					s.Errors++
				case core.SeverityWarning:
					s.Warnings++
				}
				if identity := diagnosticFileIdentity(record.Diagnostic.Location); identity != "" {
					files[identity] = struct{}{}
				}
			}
		case core.RecordTestCase:
			s.TestCaseCount++
			if record.TestCase != nil {
				switch record.TestCase.Status {
				case core.TestPassed:
					s.TestPassed++
				case core.TestFailed, core.TestError:
					s.TestFailed++
				case core.TestSkipped:
					s.TestSkipped++
				}
			}
		case core.RecordTestSuite:
			s.TestSuiteCount++
		case core.RecordArtifactResult:
			s.ArtifactCount++
		}
		if record.Authority == core.AuthorityMechanical {
			s.MechanicalCount++
		} else if record.Authority == core.AuthorityAdvisory {
			s.AdvisoryCount++
		}
	}
	s.Files = len(files)
	return s
}
