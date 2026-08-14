package structuredresult

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const inspectScanChunk = 64

type InspectStatus string
type DetailsStatus string

const (
	InspectNotFound   InspectStatus = "not_found"
	InspectPending    InspectStatus = "pending"
	InspectProcessing InspectStatus = "processing"
	InspectTerminal   InspectStatus = "terminal"

	DetailsAvailable   DetailsStatus = "available"
	DetailsUnavailable DetailsStatus = "unavailable"
	DetailsCompacted   DetailsStatus = "compacted"
)

type RecordFilter struct {
	RecordKind core.RecordKind `json:"record_kind,omitempty"`
	Severity   core.Severity   `json:"severity,omitempty"`
	Path       string          `json:"path,omitempty"`
	TestStatus core.TestStatus `json:"test_status,omitempty"`
}

type InspectRequest struct {
	OperationID  string       `json:"operation_id"`
	Filter       RecordFilter `json:"filter,omitempty"`
	Continuation string       `json:"continuation,omitempty"`
	MaxRecords   int          `json:"max_records"`
}

type RecordSummary struct {
	RecordsTotal int  `json:"records_total"`
	Errors       int  `json:"errors"`
	Warnings     int  `json:"warnings"`
	Files        int  `json:"files"`
	TestPassed   int  `json:"test_passed"`
	TestFailed   int  `json:"test_failed"`
	TestSkipped  int  `json:"test_skipped"`
	Mechanical   int  `json:"mechanical_records"`
	Advisory     int  `json:"advisory_records"`
	Compacted    bool `json:"compacted"`
}

type InspectSummary struct {
	Errors                   int           `json:"errors"`
	Warnings                 int           `json:"warnings"`
	Files                    int           `json:"files"`
	TestPassed               int           `json:"test_passed"`
	TestFailed               int           `json:"test_failed"`
	TestSkipped              int           `json:"test_skipped"`
	MechanicalRecords        int           `json:"mechanical_records"`
	AdvisoryRecords          int           `json:"advisory_records"`
	RecordsReturned          int           `json:"records_returned"`
	RecordsTotalOrLowerBound int           `json:"records_total_or_lower_bound"`
	RecordsTotalExact        bool          `json:"records_total_exact"`
	Truncated                bool          `json:"truncated"`
	DetailsStatus            DetailsStatus `json:"details_status"`
}

type InspectResult struct {
	SchemaVersion int               `json:"schema_version"`
	OperationID   string            `json:"operation_id"`
	Status        InspectStatus     `json:"status"`
	DerivationKey string            `json:"derivation_key,omitempty"`
	Producer      *core.Producer    `json:"producer,omitempty"`
	ParseOutcome  core.ParseOutcome `json:"parse_outcome,omitempty"`
	Completeness  core.Completeness `json:"completeness,omitempty"`
	Summary       InspectSummary    `json:"summary"`
	Records       []core.Record     `json:"records,omitempty"`
	Continuation  string            `json:"continuation,omitempty"`
}

type InspectionRepository interface {
	FindOperationDerivation(context.Context, string) (core.Derivation, bool, error)
	GetRecordSummary(context.Context, string) (RecordSummary, bool, error)
	ListRecords(context.Context, string, RecordQuery) ([]core.Record, error)
}

type OperationDerivationBinder interface {
	BindOperationDerivation(context.Context, operation.ID, string) error
}

type Inspector struct {
	repository InspectionRepository
	codec      *ResultCursorCodec
}

func NewInspector(repository InspectionRepository, codec *ResultCursorCodec) *Inspector {
	return &Inspector{repository: repository, codec: codec}
}

func (r InspectRequest) Validate() error {
	if _, err := operation.ParseID(r.OperationID); err != nil {
		return err
	}
	if r.MaxRecords < 1 || r.MaxRecords > MaxListRecords {
		return fmt.Errorf("invalid max_records")
	}
	if len(r.Continuation) > MaxResultCursorBytes {
		return fmt.Errorf("invalid continuation")
	}
	return r.Filter.Validate()
}

func (f RecordFilter) Validate() error {
	if f.RecordKind != "" && !validRecordKind(f.RecordKind) {
		return fmt.Errorf("invalid record_kind")
	}
	if f.Severity != "" && !validSeverity(f.Severity) {
		return fmt.Errorf("invalid severity")
	}
	if f.TestStatus != "" && !validTestStatus(f.TestStatus) {
		return fmt.Errorf("invalid test_status")
	}
	if f.Path != "" && !validFilterPath(f.Path) {
		return fmt.Errorf("invalid path")
	}
	if f.Severity != "" && f.TestStatus != "" {
		return fmt.Errorf("incompatible filters")
	}
	if f.RecordKind != "" && f.Severity != "" && f.RecordKind != core.RecordDiagnostic {
		return fmt.Errorf("incompatible filters")
	}
	if f.RecordKind != "" && f.Path != "" && f.RecordKind != core.RecordDiagnostic {
		return fmt.Errorf("incompatible filters")
	}
	if f.RecordKind != "" && f.TestStatus != "" && f.RecordKind != core.RecordTestCase && f.RecordKind != core.RecordTestSuite {
		return fmt.Errorf("incompatible filters")
	}
	return nil
}

func (s *Inspector) Inspect(ctx context.Context, request InspectRequest) (InspectResult, error) {
	if s == nil || s.repository == nil || s.codec == nil {
		return InspectResult{}, fmt.Errorf("structured inspection unavailable")
	}
	if err := request.Validate(); err != nil {
		return InspectResult{}, err
	}
	derivation, found, err := s.repository.FindOperationDerivation(ctx, request.OperationID)
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return InspectResult{SchemaVersion: 1, OperationID: request.OperationID, Status: InspectNotFound, Summary: InspectSummary{DetailsStatus: DetailsUnavailable}}, nil
	}
	result := inspectFromDerivation(request.OperationID, derivation)
	if derivation.Lifecycle != core.LifecycleTerminal {
		return result, nil
	}
	summary, summaryFound, err := s.repository.GetRecordSummary(ctx, derivation.DerivationKey)
	if err != nil {
		return InspectResult{}, err
	}
	if summaryFound {
		applyStoredSummary(&result.Summary, summary)
	}
	if derivation.Completeness == core.CompletenessCompacted || summary.Compacted {
		result.Summary.DetailsStatus = DetailsCompacted
		result.Summary.RecordsTotalExact = true
		return result, nil
	}
	result.Summary.DetailsStatus = DetailsAvailable
	if !summaryFound || summary.RecordsTotal == 0 {
		result.Summary.RecordsTotalExact = true
		return result, nil
	}
	start := 0
	if request.Continuation != "" {
		start, err = s.codec.Decode(request.Continuation, request.OperationID, derivation.DerivationKey, request.Filter)
		if err != nil {
			return InspectResult{}, err
		}
	}
	records, total, err := s.filteredPage(ctx, derivation.DerivationKey, request.Filter, start, request.MaxRecords, summary.RecordsTotal)
	if err != nil {
		return InspectResult{}, err
	}
	result.Records = records
	result.Summary.RecordsReturned = len(records)
	result.Summary.RecordsTotalOrLowerBound = total
	result.Summary.RecordsTotalExact = true
	next := start + len(records)
	result.Summary.Truncated = next < total
	if result.Summary.Truncated {
		result.Continuation, err = s.codec.Encode(request.OperationID, derivation.DerivationKey, request.Filter, next)
		if err != nil {
			return InspectResult{}, err
		}
	}
	return result, nil
}

func (s *Inspector) filteredPage(ctx context.Context, key string, filter RecordFilter, start, max, totalRecords int) ([]core.Record, int, error) {
	returned := make([]core.Record, 0, max)
	matches := 0
	for offset := 0; offset < totalRecords; offset += inspectScanChunk {
		limit := min(inspectScanChunk, totalRecords-offset)
		batch, err := s.repository.ListRecords(ctx, key, RecordQuery{Offset: offset, Limit: limit})
		if err != nil {
			return nil, 0, err
		}
		if len(batch) == 0 && limit > 0 {
			return nil, 0, fmt.Errorf("structured record summary mismatch")
		}
		for _, record := range batch {
			if !recordMatches(record, filter) {
				continue
			}
			if matches >= start && len(returned) < max {
				returned = append(returned, record)
			}
			matches++
		}
	}
	if start > matches {
		return nil, 0, fmt.Errorf("structured continuation beyond result")
	}
	return returned, matches, nil
}

func inspectFromDerivation(operationID string, d core.Derivation) InspectResult {
	status := InspectPending
	if d.Lifecycle == core.LifecycleProcessing {
		status = InspectProcessing
	} else if d.Lifecycle == core.LifecycleTerminal {
		status = InspectTerminal
	}
	producer := d.Producer
	return InspectResult{SchemaVersion: 1, OperationID: operationID, Status: status, DerivationKey: d.DerivationKey, Producer: &producer, ParseOutcome: d.ParseOutcome, Completeness: d.Completeness, Summary: InspectSummary{DetailsStatus: DetailsUnavailable}}
}
func applyStoredSummary(out *InspectSummary, in RecordSummary) {
	out.Errors = in.Errors
	out.Warnings = in.Warnings
	out.Files = in.Files
	out.TestPassed = in.TestPassed
	out.TestFailed = in.TestFailed
	out.TestSkipped = in.TestSkipped
	out.MechanicalRecords = in.Mechanical
	out.AdvisoryRecords = in.Advisory
	out.RecordsTotalOrLowerBound = in.RecordsTotal
}
func recordMatches(record core.Record, filter RecordFilter) bool {
	if filter.RecordKind != "" && record.RecordKind != filter.RecordKind {
		return false
	}
	if filter.Severity != "" {
		if record.Diagnostic == nil || record.Diagnostic.Severity != filter.Severity {
			return false
		}
	}
	if filter.Path != "" {
		if record.Diagnostic == nil || record.Diagnostic.Location.Kind != source.LocationProviderReported || record.Diagnostic.Location.ProviderReported == nil || record.Diagnostic.Location.ProviderReported.SanitizedLogicalPath != filter.Path {
			return false
		}
	}
	if filter.TestStatus != "" {
		status := ""
		if record.TestCase != nil {
			status = string(record.TestCase.Status)
		} else if record.TestSuite != nil {
			status = string(record.TestSuite.Status)
		}
		if status != string(filter.TestStatus) {
			return false
		}
	}
	return true
}
func validRecordKind(v core.RecordKind) bool {
	switch v {
	case core.RecordDiagnostic, core.RecordTestCase, core.RecordTestSuite, core.RecordArtifactResult:
		return true
	}
	return false
}
func validSeverity(v core.Severity) bool {
	switch v {
	case core.SeverityError, core.SeverityWarning, core.SeverityInfo:
		return true
	}
	return false
}
func validTestStatus(v core.TestStatus) bool {
	switch v {
	case core.TestPassed, core.TestFailed, core.TestSkipped, core.TestError:
		return true
	}
	return false
}
func validFilterPath(v string) bool {
	if len(v) < 1 || len(v) > source.MaxLogicalPathBytes || filepath.IsAbs(v) || v == "." || v == ".." || filepath.Clean(v) != v || strings.HasPrefix(v, ".."+string(filepath.Separator)) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
