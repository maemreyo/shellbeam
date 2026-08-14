package gojson

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const goVetAdapterID = "go-vet-json"

type VetAdapter struct{}

func (VetAdapter) ID() string   { return goVetAdapterID }
func (VetAdapter) Version() int { return 1 }

func (VetAdapter) Parse(ctx context.Context, ref core.RawOutputRef, reader app.Reader, limits app.Limits) (app.ParseResult, error) {
	input, err := parserContext(ctx, reader, ref, limits)
	if err != nil {
		return app.ParseResult{}, err
	}
	decoder := json.NewDecoder(newBoundedRangeReader(ctx, reader, ref, limits))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return completeResult(nil, app.ParseSummary{}), nil
		}
		return failureResult(nil, app.ParseSummary{}, err), nil
	}
	if err := checkRawDepth(raw, limits.MaxDepth); err != nil {
		return failureResult(nil, app.ParseSummary{}, err), nil
	}
	var payload map[string]map[string][]vetDiagnostic
	if err := json.Unmarshal(raw, &payload); err != nil {
		return failureResult(nil, app.ParseSummary{}, err), nil
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errParseMalformed
		}
		return failureResult(nil, app.ParseSummary{}, err), nil
	}
	return buildVetResult(payload, input, ref, limits)
}

type vetDiagnostic struct {
	Posn    string `json:"posn"`
	End     string `json:"end"`
	Message string `json:"message"`
}

func buildVetResult(payload map[string]map[string][]vetDiagnostic, input app.InputContext, ref core.RawOutputRef, limits app.Limits) (app.ParseResult, error) {
	producer := core.Producer{AdapterID: goVetAdapterID, AdapterVersion: 1, CapabilityVersion: adapterCapabilityVersion}
	packages := sortedKeys(payload)
	records := make([]core.Record, 0, min(limits.MaxRecords, 32))
	summary := app.ParseSummary{}
	for _, pkg := range packages {
		if err := checkSemanticString(pkg, limits.MaxStringBytes, false); err != nil {
			return failureResult(records, summary, err), nil
		}
		for _, analyzer := range sortedKeys(payload[pkg]) {
			if err := checkSemanticString(analyzer, min(limits.MaxStringBytes, 256), false); err != nil {
				return failureResult(records, summary, err), nil
			}
			for _, diagnostic := range payload[pkg][analyzer] {
				record, err := vetRecord(diagnostic, analyzer, input, ref, producer, limits)
				if err != nil {
					return failureResult(records, summary, err), nil
				}
				records, err = appendRecord(records, record, limits)
				if err != nil {
					return failureResult(records, summary, err), nil
				}
				summary.Errors++
			}
		}
	}
	return completeResult(records, summary), nil
}

func vetRecord(diagnostic vetDiagnostic, analyzer string, input app.InputContext, ref core.RawOutputRef, producer core.Producer, limits app.Limits) (core.Record, error) {
	for _, value := range []string{diagnostic.Posn, diagnostic.End, diagnostic.Message} {
		if err := checkSemanticString(value, limits.MaxStringBytes, value == diagnostic.End); err != nil {
			return core.Record{}, err
		}
	}
	location, err := providerLocation(diagnostic.Posn, diagnostic.End, input)
	if err != nil {
		return core.Record{}, err
	}
	record := core.Record{
		SchemaVersion: core.SchemaVersion, RecordKind: core.RecordDiagnostic,
		Authority: authorityForMethods(core.DerivationNativeFieldMapping), DerivationMethod: core.DerivationNativeFieldMapping,
		Producer: producer, OperationID: input.OperationID, SourceRef: ref,
		Diagnostic: &core.Diagnostic{Severity: core.SeverityError, Code: analyzer, Message: diagnostic.Message, Location: location},
	}
	return record, nil
}

func providerLocation(posn, end string, input app.InputContext) (source.SourceLocation, error) {
	path, line, column, err := parsePosition(posn)
	if err != nil {
		return source.SourceLocation{}, errParseMalformed
	}
	origin, logical, quality := classifyProviderPath(path, input)
	reported := &source.ProviderReportedLocation{Origin: origin, SanitizedLogicalPath: logical, Line: line, Column: column, NormalizationQuality: quality}
	if end != "" {
		endPath, endLine, endColumn, endErr := parsePosition(end)
		if endErr != nil || endPath != path {
			return source.SourceLocation{}, errParseMalformed
		}
		reported.EndLine, reported.EndColumn = endLine, endColumn
	}
	location := source.SourceLocation{Kind: source.LocationProviderReported, ProviderReported: reported}
	if err := location.Validate(); err != nil {
		return source.SourceLocation{}, errParseMalformed
	}
	return location, nil
}

func parsePosition(value string) (string, int, int, error) {
	last := strings.LastIndexByte(value, ':')
	if last <= 0 {
		return "", 0, 0, errParseMalformed
	}
	second := strings.LastIndexByte(value[:last], ':')
	if second <= 0 {
		return "", 0, 0, errParseMalformed
	}
	line, errLine := strconv.Atoi(value[second+1 : last])
	column, errColumn := strconv.Atoi(value[last+1:])
	if errLine != nil || errColumn != nil || line < 1 || column < 1 {
		return "", 0, 0, errParseMalformed
	}
	return value[:second], line, column, nil
}

func classifyProviderPath(path string, input app.InputContext) (source.Origin, string, source.NormalizationQuality) {
	if !filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return source.OriginRepository, filepath.ToSlash(clean), source.NormalizationPartial
		}
		return source.OriginExternal, filepath.Base(clean), source.NormalizationUnavailable
	}
	if logical, ok := pathWithin(path, input.RepositoryRoot); ok {
		return source.OriginRepository, logical, source.NormalizationPartial
	}
	for _, root := range input.DependencyRoots {
		if logical, ok := pathWithin(path, root); ok {
			return source.OriginDependency, logical, source.NormalizationPartial
		}
	}
	for _, root := range input.ToolchainRoots {
		if logical, ok := pathWithin(path, root); ok {
			return source.OriginToolchain, logical, source.NormalizationPartial
		}
	}
	return source.OriginExternal, filepath.Base(path), source.NormalizationUnavailable
}

func pathWithin(path, root string) (string, bool) {
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(rel)), true
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ app.Adapter = VetAdapter{}
