package jestjson

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type Adapter struct{}

func (Adapter) ID() string   { return "jest-json" }
func (Adapter) Version() int { return adapterVersion }

func (Adapter) Parse(ctx context.Context, ref core.StructuredInputRef, reader app.Reader, limits app.Limits) (app.ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return app.ParseResult{}, err
	}
	if reader == nil || limits.Validate() != nil || ref.Validate() != nil || ref.Kind != core.StructuredInputArtifactBlob || ref.ArtifactBlob == nil {
		return app.ParseResult{}, fmt.Errorf("jest json adapter requires artifact input")
	}
	parseCtx, cancel := context.WithTimeout(ctx, limits.MaxDuration)
	defer cancel()
	input, err := reader.DescribeInput(parseCtx, ref)
	if err != nil {
		return app.ParseResult{}, err
	}
	if err := input.Validate(); err != nil || input.DerivationKey == "" || input.OperationID != ref.ArtifactBlob.OperationID {
		return app.ParseResult{}, fmt.Errorf("jest json derivation context unavailable")
	}
	if ref.ArtifactBlob.Size <= 0 || ref.ArtifactBlob.Size > limits.MaxBytes || ref.ArtifactBlob.Size > int64(^uint(0)>>1) {
		return app.ParseResult{Outcome: core.ParseBudgetExceeded, Completeness: core.CompletenessUnavailable}, nil
	}
	data, err := reader.ReadInputRange(parseCtx, ref, 0, int(ref.ArtifactBlob.Size))
	if err != nil {
		return app.ParseResult{}, err
	}
	if int64(len(data)) != ref.ArtifactBlob.Size {
		return app.ParseResult{Outcome: core.ParseMalformed, Completeness: core.CompletenessUnavailable}, nil
	}
	if err := parseCtx.Err(); err != nil {
		return app.ParseResult{}, err
	}
	profile, outcome := decodeProfile(data)
	if outcome != core.ParseComplete {
		return app.ParseResult{Outcome: outcome, Completeness: core.CompletenessUnavailable}, nil
	}
	withinBounds, err := validateProfileBounds(parseCtx, profile, min(limits.MaxStringBytes, maxJestStringBytes))
	if err != nil {
		return app.ParseResult{}, err
	}
	if !withinBounds {
		return app.ParseResult{Outcome: core.ParseBudgetExceeded, Completeness: core.CompletenessUnavailable}, nil
	}
	result, ok, err := normalizeProfile(parseCtx, profile, ref, input)
	if err != nil {
		return app.ParseResult{}, err
	}
	if !ok {
		return result, nil
	}
	if result.ObservedEntries != nil && result.ObservedEntries.Files == 0 && result.ObservedEntries.Entries == 0 {
		result.Outcome = core.ParsePartial
		result.Completeness = core.CompletenessPartial
		result.CompletenessReason = core.CompletenessReasonZeroMatch
		return result, nil
	}
	selection, err := core.SelectRecordsFailureFirst(result.Records, core.RecordBudget{MaxRecords: min(limits.MaxRecords, maxJestJSONRecords)})
	if err != nil {
		return app.ParseResult{}, err
	}
	result.Records = selection.Records
	if selection.Outcome != core.ParseComplete {
		result.Outcome = selection.Outcome
		result.Completeness = selection.Completeness
		result.CompletenessReason = selection.CompletenessReason
	} else if result.Outcome != core.ParsePartial {
		result.Outcome = selection.Outcome
		result.Completeness = selection.Completeness
		result.CompletenessReason = selection.CompletenessReason
	}
	result.Summary = summarizeRecords(result.Records)
	return result, nil
}
