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
	records := make([]core.Record, 0, min(s.limits.Result.MaxRecords, providerRecordCount(query, response)))
	bounded := false
	changedRecord := false
	appendRecord := func(record core.Record) error {
		if err := record.Validate(s.limits.Result); err != nil {
			return err
		}
		if len(records) >= s.limits.Result.MaxRecords {
			bounded = true
			return nil
		}
		records = append(records, record)
		return nil
	}

	switch query.Kind {
	case core.QueryDiagnostics:
		for _, diagnostic := range response.Diagnostics {
			if err := validateProviderLocation(diagnostic.Location); err != nil {
				return nil, false, false, err
			}
			if !recordVisibleForSelection(query, diagnostic.Location, selectedIDs) {
				continue
			}
			for _, related := range diagnostic.RelatedLocations {
				if err := validateProviderLocation(related); err != nil {
					return nil, false, false, err
				}
			}
			correlation := correlationForLocation(diagnostic.Location, selectedIDs, changed)
			changedRecord = changedRecord || correlation == core.CorrelationSourceChangedDuringQuery
			record := core.Record{
				Kind:              core.RecordDiagnostic,
				Authority:         defaultAuthority(diagnostic.Authority),
				SourceCorrelation: correlation,
				Completeness:      defaultCompleteness(diagnostic.Completeness),
				Diagnostic: &core.Diagnostic{
					Severity:         diagnostic.Severity,
					Code:             diagnostic.Code,
					Message:          diagnostic.Message,
					Location:         diagnostic.Location,
					ProviderSource:   diagnostic.ProviderSource,
					RelatedLocations: append([]core.SourceLocation(nil), diagnostic.RelatedLocations...),
				},
			}
			if err := appendRecord(record); err != nil {
				return nil, false, false, err
			}
		}
	case core.QuerySymbols:
		for _, symbol := range response.Symbols {
			if err := validateProviderLocation(symbol.Location); err != nil {
				return nil, false, false, err
			}
			if !recordVisibleForSelection(query, symbol.Location, selectedIDs) {
				continue
			}
			correlation := correlationForLocation(symbol.Location, selectedIDs, changed)
			changedRecord = changedRecord || correlation == core.CorrelationSourceChangedDuringQuery
			record := core.Record{
				Kind:              core.RecordSymbol,
				Authority:         defaultAuthority(symbol.Authority),
				SourceCorrelation: correlation,
				Completeness:      defaultCompleteness(symbol.Completeness),
				Symbol:            &core.Symbol{Name: symbol.Name, Kind: symbol.Kind, Location: symbol.Location},
			}
			if err := appendRecord(record); err != nil {
				return nil, false, false, err
			}
		}
	case core.QueryDefinition, core.QueryReferences, core.QueryTypeDefinition, core.QueryCallers, core.QueryCallees:
		for _, location := range response.Locations {
			if err := validateProviderLocation(location.Location); err != nil {
				return nil, false, false, err
			}
			correlation := correlationForLocation(location.Location, selectedIDs, changed)
			changedRecord = changedRecord || correlation == core.CorrelationSourceChangedDuringQuery
			record := core.Record{
				Kind:              core.RecordLocationTarget,
				Authority:         defaultAuthority(location.Authority),
				SourceCorrelation: correlation,
				Completeness:      defaultCompleteness(location.Completeness),
				LocationTarget: &core.LocationTarget{
					Name:         location.Name,
					Relationship: defaultRelationship(query.Kind, location.Relationship),
					Location:     location.Location,
				},
			}
			if err := appendRecord(record); err != nil {
				return nil, false, false, err
			}
		}
	case core.QueryTypeSummary:
		if response.TypeSummary != "" {
			record := core.Record{
				Kind:              core.RecordTypeSummary,
				Authority:         core.AuthorityMechanical,
				SourceCorrelation: core.CorrelationCurrent,
				Completeness:      core.CompletenessProviderReported,
				TypeSummary:       &core.TypeSummary{Text: response.TypeSummary},
			}
			if err := appendRecord(record); err != nil {
				return nil, false, false, err
			}
		}
	}
	return records, bounded, changedRecord, nil
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
