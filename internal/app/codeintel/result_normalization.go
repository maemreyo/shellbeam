package codeintel

import (
	"encoding/json"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

func (s *Service) normalizeRecords(query core.Query, response ProviderResponse, selected []BoundSource, changed map[core.SourceRefID]bool) ([]core.Record, bool, bool, error) {
	selectedIDs := make(map[core.SourceRefID]struct{}, len(selected))
	for _, source := range selected {
		selectedIDs[source.Ref.ID] = struct{}{}
	}
	state := recordNormalizationState{
		limits:      s.limits.Result,
		selectedIDs: selectedIDs,
		changed:     changed,
		records:     make([]core.Record, 0, min(s.limits.Result.MaxRecords, providerRecordCount(query, response))),
	}
	var err error
	switch query.Kind {
	case core.QueryDiagnostics:
		err = state.appendDiagnostics(query, response.Diagnostics)
	case core.QuerySymbols:
		err = state.appendSymbols(query, response.Symbols)
	case core.QueryDefinition, core.QueryReferences, core.QueryTypeDefinition, core.QueryCallers, core.QueryCallees:
		err = state.appendLocations(query, response.Locations)
	case core.QueryTypeSummary:
		err = state.appendTypeSummary(response.TypeSummary)
	}
	return state.records, state.bounded, state.changedRecord, err
}

type recordNormalizationState struct {
	limits        core.ResultLimits
	selectedIDs   map[core.SourceRefID]struct{}
	changed       map[core.SourceRefID]bool
	records       []core.Record
	bounded       bool
	changedRecord bool
}

func (s *recordNormalizationState) append(record core.Record) error {
	if err := record.Validate(s.limits); err != nil {
		return err
	}
	if len(s.records) >= s.limits.MaxRecords {
		s.bounded = true
		return nil
	}
	s.records = append(s.records, record)
	return nil
}

func (s *recordNormalizationState) appendDiagnostics(query core.Query, diagnostics []ProviderDiagnostic) error {
	for _, diagnostic := range diagnostics {
		if err := validateProviderLocation(diagnostic.Location); err != nil {
			return err
		}
		if !recordVisibleForSelection(query, diagnostic.Location, s.selectedIDs) {
			continue
		}
		for _, related := range diagnostic.RelatedLocations {
			if err := validateProviderLocation(related); err != nil {
				return err
			}
		}
		correlation := s.correlation(diagnostic.Location)
		record := core.Record{
			Kind:              core.RecordDiagnostic,
			Authority:         defaultAuthority(diagnostic.Authority),
			SourceCorrelation: correlation,
			Completeness:      defaultCompleteness(diagnostic.Completeness),
			Diagnostic: &core.Diagnostic{
				Severity: diagnostic.Severity, Code: diagnostic.Code, Message: diagnostic.Message,
				Location: diagnostic.Location, ProviderSource: diagnostic.ProviderSource,
				RelatedLocations: append([]core.SourceLocation(nil), diagnostic.RelatedLocations...),
			},
		}
		if err := s.append(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *recordNormalizationState) appendSymbols(query core.Query, symbols []ProviderSymbol) error {
	for _, symbol := range symbols {
		if err := validateProviderLocation(symbol.Location); err != nil {
			return err
		}
		if !recordVisibleForSelection(query, symbol.Location, s.selectedIDs) {
			continue
		}
		record := core.Record{
			Kind: core.RecordSymbol, Authority: defaultAuthority(symbol.Authority),
			SourceCorrelation: s.correlation(symbol.Location), Completeness: defaultCompleteness(symbol.Completeness),
			Symbol: &core.Symbol{Name: symbol.Name, Kind: symbol.Kind, Location: symbol.Location},
		}
		if err := s.append(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *recordNormalizationState) appendLocations(query core.Query, locations []ProviderLocation) error {
	for _, location := range locations {
		if err := validateProviderLocation(location.Location); err != nil {
			return err
		}
		record := core.Record{
			Kind: core.RecordLocationTarget, Authority: defaultAuthority(location.Authority),
			SourceCorrelation: s.correlation(location.Location), Completeness: defaultCompleteness(location.Completeness),
			LocationTarget: &core.LocationTarget{
				Name: location.Name, Relationship: defaultRelationship(query.Kind, location.Relationship), Location: location.Location,
			},
		}
		if err := s.append(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *recordNormalizationState) appendTypeSummary(summary string) error {
	if summary == "" {
		return nil
	}
	return s.append(core.Record{
		Kind: core.RecordTypeSummary, Authority: core.AuthorityMechanical,
		SourceCorrelation: core.CorrelationCurrent, Completeness: core.CompletenessProviderReported,
		TypeSummary: &core.TypeSummary{Text: summary},
	})
}

func (s *recordNormalizationState) correlation(location core.SourceLocation) core.SourceCorrelation {
	correlation := correlationForLocation(location, s.selectedIDs, s.changed)
	if correlation == core.CorrelationSourceChangedDuringQuery {
		s.changedRecord = true
	}
	return correlation
}

func (s *Service) fitResult(result core.Result) (core.Result, error) {
	for {
		encoded, err := json.Marshal(result)
		if err != nil {
			return core.Result{}, err
		}
		if len(encoded) <= s.limits.Result.MaxResponseBytes {
			if err := result.Validate(s.limits.Result); err != nil {
				return core.Result{}, err
			}
			return result, nil
		}
		if len(result.Records) == 0 {
			return core.Result{}, newError(CodeQueryBudgetExceeded, false, fmt.Errorf("code result byte budget exceeded"))
		}
		result.Records = result.Records[:len(result.Records)-1]
		result.Status = partialStatus(result.Status)
		degradeSelectionForBudget(&result.Selection)
	}
}

func validateProviderLocation(location core.SourceLocation) error {
	if err := location.Validate(); err != nil {
		return fmt.Errorf("invalid provider location: %w", err)
	}
	return nil
}

func recordVisibleForSelection(query core.Query, location core.SourceLocation, selected map[core.SourceRefID]struct{}) bool {
	if query.Scope != core.ScopeChangedFiles && query.Scope != core.ScopeFile {
		return true
	}
	if location.Kind != core.LocationResolved || location.Resolved == nil {
		return false
	}
	_, ok := selected[core.SourceRefID(location.Resolved.SourceRefID)]
	return ok
}

func correlationForLocation(location core.SourceLocation, selected map[core.SourceRefID]struct{}, changed map[core.SourceRefID]bool) core.SourceCorrelation {
	if location.Kind != core.LocationResolved || location.Resolved == nil {
		return core.CorrelationUnknown
	}
	id := core.SourceRefID(location.Resolved.SourceRefID)
	if changed[id] {
		return core.CorrelationSourceChangedDuringQuery
	}
	if _, ok := selected[id]; ok {
		return core.CorrelationCurrent
	}
	return core.CorrelationUnknown
}

func defaultAuthority(authority core.Authority) core.Authority {
	if authority == "" {
		return core.AuthorityMechanical
	}
	return authority
}

func defaultCompleteness(completeness core.RecordCompleteness) core.RecordCompleteness {
	if completeness == "" {
		return core.CompletenessProviderReported
	}
	return completeness
}

func defaultRelationship(kind core.QueryKind, relationship string) string {
	if relationship != "" {
		return relationship
	}
	switch kind {
	case core.QueryDefinition:
		return "definition"
	case core.QueryReferences:
		return "reference"
	case core.QueryTypeDefinition:
		return "type_definition"
	case core.QueryCallers:
		return "caller"
	case core.QueryCallees:
		return "callee"
	default:
		return string(kind)
	}
}

func providerRecordCount(query core.Query, response ProviderResponse) int {
	switch query.Kind {
	case core.QueryDiagnostics:
		return len(response.Diagnostics)
	case core.QuerySymbols:
		return len(response.Symbols)
	case core.QueryDefinition, core.QueryReferences, core.QueryTypeDefinition, core.QueryCallers, core.QueryCallees:
		return len(response.Locations)
	case core.QueryTypeSummary:
		if response.TypeSummary != "" {
			return 1
		}
	}
	return 0
}
