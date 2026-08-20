package gojson

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const goTestAdapterID = "go-test-json"

type TestAdapter struct{}

func (TestAdapter) ID() string   { return goTestAdapterID }
func (TestAdapter) Version() int { return 1 }

func (TestAdapter) Parse(ctx context.Context, ref core.StructuredInputRef, reader app.Reader, limits app.Limits) (app.ParseResult, error) {
	input, err := parserContext(ctx, reader, ref, limits)
	if err != nil {
		return app.ParseResult{}, err
	}
	producer := core.Producer{AdapterID: goTestAdapterID, AdapterVersion: 1, CapabilityVersion: adapterCapabilityVersion}
	decoder := json.NewDecoder(newBoundedRangeReader(ctx, reader, ref, limits))
	records := make([]core.Record, 0, min(limits.MaxRecords, 32))
	summary := app.ParseSummary{}
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return completeResult(records, summary), nil
			}
			return failureResult(records, summary, err), nil
		}
		if err := checkRawDepth(raw, limits.MaxDepth); err != nil {
			return failureResult(records, summary, err), nil
		}
		var event testJSONEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return failureResult(records, summary, err), nil
		}
		if err := validateTestEvent(event, limits); err != nil {
			return failureResult(records, summary, err), nil
		}
		record, ok, err := testEventRecord(event, input.OperationID, ref, producer)
		if err != nil {
			return failureResult(records, summary, err), nil
		}
		if !ok {
			continue
		}
		records, err = appendRecord(records, record, limits)
		if err != nil {
			return failureResult(records, summary, err), nil
		}
		updateTestSummary(&summary, record)
	}
}

type testJSONEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

func validateTestEvent(event testJSONEvent, limits app.Limits) error {
	for _, value := range []string{event.Action, event.Package, event.Test, event.Time} {
		if err := checkSemanticString(value, limits.MaxStringBytes, true); err != nil {
			return err
		}
	}
	if len(event.Output) > limits.MaxStringBytes {
		return errParseBudget
	}
	if event.Time != "" {
		if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
			return errParseMalformed
		}
	}
	_, err := durationMS(event.Elapsed)
	return err
}

func testEventRecord(event testJSONEvent, operationID string, ref core.StructuredInputRef, producer core.Producer) (core.Record, bool, error) {
	status, ok := testStatus(event.Action)
	if !ok {
		return core.Record{}, false, nil
	}
	if err := checkSemanticString(event.Package, 1024, false); err != nil {
		return core.Record{}, false, err
	}
	duration, err := durationMS(event.Elapsed)
	if err != nil {
		return core.Record{}, false, err
	}
	base := core.Record{SchemaVersion: core.SchemaVersion, Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping, Producer: producer, OperationID: operationID, SourceRef: ref}
	if event.Test != "" {
		if err := checkSemanticString(event.Test, 1024, false); err != nil {
			return core.Record{}, false, err
		}
		base.RecordKind = core.RecordTestCase
		base.TestCase = &core.TestCase{Name: event.Test, Package: event.Package, Status: status, DurationMS: duration}
		return base, true, nil
	}
	base.RecordKind = core.RecordTestSuite
	base.TestSuite = &core.TestSuite{Name: event.Package, Package: event.Package, Status: status, DurationMS: duration}
	return base, true, nil
}

func testStatus(action string) (core.TestStatus, bool) {
	switch action {
	case "pass":
		return core.TestPassed, true
	case "fail":
		return core.TestFailed, true
	case "skip":
		return core.TestSkipped, true
	default:
		return "", false
	}
}

func updateTestSummary(summary *app.ParseSummary, record core.Record) {
	if record.RecordKind != core.RecordTestCase || record.TestCase == nil {
		return
	}
	switch record.TestCase.Status {
	case core.TestPassed:
		summary.TestPassed++
	case core.TestFailed, core.TestError:
		summary.TestFailed++
	case core.TestSkipped:
		summary.TestSkipped++
	}
}

var _ app.Adapter = TestAdapter{}
