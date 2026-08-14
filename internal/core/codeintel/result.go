package codeintel

import (
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type ResultStatus string
type RecordKind string
type Authority string
type SourceCorrelation string
type RecordCompleteness string
type Severity string

const (
	StatusReady       ResultStatus = "ready"
	StatusPartial     ResultStatus = "partial"
	StatusStarting    ResultStatus = "starting"
	StatusIndexing    ResultStatus = "indexing"
	StatusUnavailable ResultStatus = "unavailable"
	StatusStale       ResultStatus = "stale"
	StatusFailed      ResultStatus = "failed"

	RecordDiagnostic     RecordKind = "diagnostic"
	RecordSymbol         RecordKind = "symbol"
	RecordLocationTarget RecordKind = "location_target"
	RecordImport         RecordKind = "import"
	RecordTypeSummary    RecordKind = "type_summary"

	AuthorityMechanical Authority = "mechanical"
	AuthorityAdvisory   Authority = "advisory"

	CorrelationCurrent                  SourceCorrelation = "current"
	CorrelationStale                    SourceCorrelation = "stale"
	CorrelationSourceChangedDuringQuery SourceCorrelation = "source_changed_during_query"
	CorrelationUnknown                  SourceCorrelation = "unknown"

	CompletenessProviderReported RecordCompleteness = "provider_reported"
	CompletenessPartial          RecordCompleteness = "partial"
	CompletenessUnknown          RecordCompleteness = "unknown"
	CompletenessExhaustive       RecordCompleteness = "exhaustive"

	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type SelectionMetadata struct {
	Basis          workspace.SelectionBasis        `json:"selection_basis,omitempty"`
	Freshness      workspace.SampleFreshness       `json:"sample_freshness,omitempty"`
	Completeness   workspace.SelectionCompleteness `json:"selection_completeness,omitempty"`
	Fallback       string                          `json:"fallback_available,omitempty"`
	ManagedOverlap bool                            `json:"managed_overlap,omitempty"`
}

type Result struct {
	Status    ResultStatus      `json:"status"`
	Query     Query             `json:"query"`
	Selection SelectionMetadata `json:"selection,omitempty"`
	Provider  ProviderMetadata  `json:"provider,omitempty"`
	Records   []Record          `json:"records,omitempty"`
}

type Record struct {
	Kind              RecordKind         `json:"kind"`
	Authority         Authority          `json:"authority"`
	SourceCorrelation SourceCorrelation  `json:"source_correlation"`
	Completeness      RecordCompleteness `json:"completeness,omitempty"`
	Diagnostic        *Diagnostic        `json:"diagnostic,omitempty"`
	Symbol            *Symbol            `json:"symbol,omitempty"`
	LocationTarget    *LocationTarget    `json:"location_target,omitempty"`
	Import            *ImportRecord      `json:"import,omitempty"`
	TypeSummary       *TypeSummary       `json:"type_summary,omitempty"`
}

type Diagnostic struct {
	Severity         Severity         `json:"severity"`
	Code             string           `json:"code,omitempty"`
	Message          string           `json:"message"`
	Location         SourceLocation   `json:"location"`
	ProviderSource   string           `json:"provider_source,omitempty"`
	RelatedLocations []SourceLocation `json:"related_locations,omitempty"`
}

type Symbol struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Location SourceLocation `json:"location"`
}

type LocationTarget struct {
	Name         string         `json:"name,omitempty"`
	Relationship string         `json:"relationship"`
	Location     SourceLocation `json:"location"`
}

type ImportRecord struct {
	Declaration string          `json:"declaration,omitempty"`
	Location    SourceLocation  `json:"location"`
	Target      *SourceLocation `json:"target,omitempty"`
}

type TypeSummary struct {
	Text     string          `json:"text"`
	Location *SourceLocation `json:"location,omitempty"`
}

type ResultLimits struct {
	MaxRecords          int
	MaxResponseBytes    int
	MaxTextBytes        int
	MaxRelatedLocations int
}

func (l ResultLimits) Validate() error {
	if l.MaxRecords < 1 || l.MaxRecords > 4096 ||
		l.MaxResponseBytes < 1 || l.MaxResponseBytes > 8<<20 ||
		l.MaxTextBytes < 1 || l.MaxTextBytes > 1<<20 ||
		l.MaxRelatedLocations < 0 || l.MaxRelatedLocations > 256 {
		return fmt.Errorf("invalid code result limits")
	}
	return nil
}

func (r Result) Validate(limits ResultLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := r.Status.Validate(); err != nil {
		return err
	}
	if err := r.Query.Validate(); err != nil {
		return err
	}
	if err := r.Selection.Validate(); err != nil {
		return err
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	if statusNeedsProvider(r.Status) && r.Provider.ProviderID == "" {
		return fmt.Errorf("code result status requires provider metadata")
	}
	if len(r.Records) > limits.MaxRecords {
		return fmt.Errorf("code result record limit exceeded")
	}
	for i := range r.Records {
		if err := r.Records[i].Validate(limits); err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}
		if !recordCompatible(r.Query.Kind, r.Records[i].Kind) {
			return fmt.Errorf("record %d incompatible with query %q", i, r.Query.Kind)
		}
		if isCallHierarchy(r.Query.Kind) &&
			r.Records[i].Authority == AuthorityMechanical &&
			r.Records[i].Completeness == CompletenessExhaustive {
			return fmt.Errorf("mechanical call hierarchy cannot claim exhaustive completeness")
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode code result: %w", err)
	}
	if len(encoded) > limits.MaxResponseBytes {
		return fmt.Errorf("code result byte limit exceeded")
	}
	return nil
}

func (s ResultStatus) Validate() error {
	switch s {
	case StatusReady, StatusPartial, StatusStarting, StatusIndexing,
		StatusUnavailable, StatusStale, StatusFailed:
		return nil
	default:
		return fmt.Errorf("invalid code result status %q", s)
	}
}

func (m SelectionMetadata) Validate() error {
	if m == (SelectionMetadata{}) {
		return nil
	}
	if m.Basis != "" && m.Basis.Validate() != nil {
		return fmt.Errorf("invalid code selection basis")
	}
	if m.Freshness != "" && m.Freshness.Validate() != nil {
		return fmt.Errorf("invalid code selection freshness")
	}
	if m.Completeness != "" && m.Completeness.Validate() != nil {
		return fmt.Errorf("invalid code selection completeness")
	}
	if m.Fallback != "" && !safeBoundedText(m.Fallback, MaxProviderTextBytes) {
		return fmt.Errorf("invalid code selection fallback")
	}
	return nil
}

func (r Record) Validate(limits ResultLimits) error {
	if !validAuthority(r.Authority) || !validCorrelation(r.SourceCorrelation) ||
		(r.Completeness != "" && !validCompleteness(r.Completeness)) {
		return fmt.Errorf("invalid code record metadata")
	}
	branches := 0
	if r.Diagnostic != nil {
		branches++
		if r.Kind != RecordDiagnostic || r.Diagnostic.Validate(limits) != nil {
			return fmt.Errorf("invalid diagnostic record")
		}
	}
	if r.Symbol != nil {
		branches++
		if r.Kind != RecordSymbol || r.Symbol.Validate(limits) != nil {
			return fmt.Errorf("invalid symbol record")
		}
	}
	if r.LocationTarget != nil {
		branches++
		if r.Kind != RecordLocationTarget || r.LocationTarget.Validate(limits) != nil {
			return fmt.Errorf("invalid location target record")
		}
	}
	if r.Import != nil {
		branches++
		if r.Kind != RecordImport || r.Import.Validate(limits) != nil {
			return fmt.Errorf("invalid import record")
		}
	}
	if r.TypeSummary != nil {
		branches++
		if r.Kind != RecordTypeSummary || r.TypeSummary.Validate(limits) != nil {
			return fmt.Errorf("invalid type summary record")
		}
	}
	if branches != 1 {
		return fmt.Errorf("code record requires exactly one branch")
	}
	return nil
}

func (d Diagnostic) Validate(limits ResultLimits) error {
	if !validSeverity(d.Severity) ||
		!safeBoundedText(d.Message, limits.MaxTextBytes) ||
		(d.Code != "" && !safeBoundedText(d.Code, limits.MaxTextBytes)) ||
		(d.ProviderSource != "" && !safeBoundedText(d.ProviderSource, limits.MaxTextBytes)) ||
		len(d.RelatedLocations) > limits.MaxRelatedLocations {
		return fmt.Errorf("invalid code diagnostic")
	}
	if err := d.Location.Validate(); err != nil {
		return err
	}
	for _, location := range d.RelatedLocations {
		if err := location.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s Symbol) Validate(limits ResultLimits) error {
	if !safeBoundedText(s.Name, limits.MaxTextBytes) ||
		!safeBoundedText(s.Kind, limits.MaxTextBytes) {
		return fmt.Errorf("invalid code symbol")
	}
	return s.Location.Validate()
}

func (t LocationTarget) Validate(limits ResultLimits) error {
	if (t.Name != "" && !safeBoundedText(t.Name, limits.MaxTextBytes)) ||
		!safeBoundedText(t.Relationship, limits.MaxTextBytes) {
		return fmt.Errorf("invalid location target")
	}
	return t.Location.Validate()
}

func (i ImportRecord) Validate(limits ResultLimits) error {
	if i.Declaration != "" && !safeBoundedText(i.Declaration, limits.MaxTextBytes) {
		return fmt.Errorf("invalid import declaration")
	}
	if err := i.Location.Validate(); err != nil {
		return err
	}
	if i.Target != nil {
		return i.Target.Validate()
	}
	return nil
}

func (s TypeSummary) Validate(limits ResultLimits) error {
	if !safeBoundedText(s.Text, limits.MaxTextBytes) {
		return fmt.Errorf("invalid type summary")
	}
	if s.Location != nil {
		return s.Location.Validate()
	}
	return nil
}

func recordCompatible(query QueryKind, record RecordKind) bool {
	switch query {
	case QueryDiagnostics:
		return record == RecordDiagnostic
	case QuerySymbols:
		return record == RecordSymbol
	case QueryImportDeclarations, QueryResolvedImportTargets:
		return record == RecordImport
	case QueryTypeSummary:
		return record == RecordTypeSummary
	default:
		return record == RecordLocationTarget
	}
}

func statusNeedsProvider(status ResultStatus) bool {
	switch status {
	case StatusReady, StatusPartial, StatusStarting, StatusIndexing, StatusStale:
		return true
	default:
		return false
	}
}

func isCallHierarchy(kind QueryKind) bool {
	return kind == QueryCallers || kind == QueryCallees
}

func validAuthority(v Authority) bool {
	return v == AuthorityMechanical || v == AuthorityAdvisory
}

func validCorrelation(v SourceCorrelation) bool {
	switch v {
	case CorrelationCurrent, CorrelationStale, CorrelationSourceChangedDuringQuery, CorrelationUnknown:
		return true
	default:
		return false
	}
}

func validCompleteness(v RecordCompleteness) bool {
	switch v {
	case CompletenessProviderReported, CompletenessPartial, CompletenessUnknown, CompletenessExhaustive:
		return true
	default:
		return false
	}
}

func validSeverity(v Severity) bool {
	return v == SeverityError || v == SeverityWarning || v == SeverityInfo
}
