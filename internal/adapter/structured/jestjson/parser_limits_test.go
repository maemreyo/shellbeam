package jestjson

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestJestJSONSuiteErrorsNeverSynthesizeCoreErrorStatus(t *testing.T) {
	f := false
	start := int64(0)
	cases := []struct {
		name       string
		assertions []assertionV30
		wantCases  []core.TestStatus
	}{
		{"beforeAll", []assertionV30{{Title: "a", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1}, {Title: "b", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1}}, []core.TestStatus{core.TestFailed, core.TestFailed}},
		{"afterAll", []assertionV30{{Title: "a", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}}, []core.TestStatus{core.TestPassed}},
		{"module throw", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBytes(t, syntheticV30(t, "failed", tc.assertions))
			suites, tests := jestSuiteRecords(result), jestTestcaseRecords(result)
			if result.Outcome != core.ParseComplete || len(suites) != 1 || suites[0].TestSuite.Status != core.TestFailed || len(tests) != len(tc.wantCases) {
				t.Fatalf("result=%#v suites=%#v tests=%#v", result, suites, tests)
			}
			for i, record := range tests {
				if record.TestCase.Status != tc.wantCases[i] || record.TestCase.Status == core.TestError {
					t.Fatalf("test[%d]=%#v", i, record.TestCase)
				}
			}
		})
	}
}

func TestJestJSONFailureFirstBudgetRetainsLateFailuresAndFullObservedCounts(t *testing.T) {
	f := false
	start := int64(0)
	assertions := make([]assertionV30, 0, 7)
	for i := 0; i < 5; i++ {
		assertions = append(assertions, assertionV30{Title: "pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1})
	}
	assertions = append(assertions,
		assertionV30{Title: "late fail one", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1},
		assertionV30{Title: "late fail two", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1},
	)
	reader, ref := newArtifactReader(syntheticV30(t, "failed", assertions))
	limits := jestLimits()
	limits.MaxRecords = 4
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial || result.CompletenessReason != core.CompletenessReasonPassRecordsElided {
		t.Fatalf("result=%#v", result)
	}
	if got := result.ObservedEntries; got == nil || got.Files != 1 || got.Entries != 7 || got.Pass != 5 || got.Fail != 2 || got.Skip != 0 || got.Error != 0 {
		t.Fatalf("observed=%#v", got)
	}
	if len(result.Records) != 4 {
		t.Fatalf("records=%d %#v", len(result.Records), result.Records)
	}
	seenFailures := map[string]bool{}
	for _, record := range result.Records {
		if record.TestCase != nil && record.TestCase.Status == core.TestFailed {
			seenFailures[record.TestCase.Name] = true
		}
	}
	if !seenFailures["late fail one"] || !seenFailures["late fail two"] {
		t.Fatalf("late failures not retained: %#v", result.Records)
	}
}

func parseWithLimits(t *testing.T, data []byte, limits app.Limits) app.ParseResult {
	t.Helper()
	reader, ref := newArtifactReader(data)
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestJestJSONZeroMatchRequiresZeroFilesAndZeroEntries(t *testing.T) {
	empty := documentV30{OpenHandles: []byte(`[]`), Snapshot: []byte(`{}`), Success: true, TestResults: []testResultV30{}}
	data, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	zero := parseBytes(t, data)
	if zero.Outcome != core.ParsePartial || zero.Completeness != core.CompletenessPartial || zero.CompletenessReason != core.CompletenessReasonZeroMatch || zero.ObservedEntries == nil || zero.ObservedEntries.Files != 0 || zero.ObservedEntries.Entries != 0 {
		t.Fatalf("zero-match=%#v", zero)
	}
	if zero.SemanticsCoverage == nil || zero.SemanticsCoverage.Family != "v29" || !containsString(zero.SemanticsCoverage.Unavailable, "jest:failing_expected") {
		t.Fatalf("zero-match claimed unobserved v30 discriminator: %#v", zero.SemanticsCoverage)
	}

	moduleError := parseBytes(t, syntheticV30(t, "failed", nil))
	if moduleError.Outcome != core.ParseComplete || moduleError.Completeness != core.CompletenessComplete || moduleError.CompletenessReason != "" || moduleError.ObservedEntries == nil || moduleError.ObservedEntries.Files != 1 || moduleError.ObservedEntries.Entries != 0 {
		t.Fatalf("module error mislabeled zero-match: %#v", moduleError)
	}
}

func TestJestJSONRejectsStringAndObservedEntryBudgetBeforeNormalization(t *testing.T) {
	f := false
	start := int64(0)
	tooLong := assertionV30{Title: strings.Repeat("x", maxJestStringBytes+1), Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}
	result := parseBytes(t, syntheticV30(t, "passed", []assertionV30{tooLong}))
	if result.Outcome != core.ParseBudgetExceeded || result.Completeness != core.CompletenessUnavailable || len(result.Records) != 0 {
		t.Fatalf("string budget=%#v", result)
	}

	assertions := make([]assertionV30, core.MaxObservedEntries+1)
	for i := range assertions {
		assertions[i] = assertionV30{Title: "x", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}
	}
	assertions[0].Status = "disabled" // ceiling must win before semantic normalization.
	data := syntheticV30(t, "passed", assertions)
	reader, ref := newArtifactReader(data)
	limits := jestLimits()
	limits.MaxBytes = 64 << 20
	// Race instrumentation makes the 65k-entry structural-ceiling fixture
	// intentionally expensive. Keep this test focused on ceiling-before-
	// normalization ordering rather than the independent duration budget.
	limits.MaxDuration = 10 * time.Second
	result, parseErr := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if result.Outcome != core.ParseBudgetExceeded || result.Completeness != core.CompletenessUnavailable || len(result.Records) != 0 {
		t.Fatalf("entry ceiling=%#v", result)
	}
}

type deadlineRequiredReader struct {
	*memoryArtifactReader
}

func (r *deadlineRequiredReader) DescribeInput(ctx context.Context, ref core.StructuredInputRef) (app.InputContext, error) {
	if _, ok := ctx.Deadline(); !ok {
		return app.InputContext{}, errors.New("parse context missing deadline")
	}
	return r.memoryArtifactReader.DescribeInput(ctx, ref)
}

func (r *deadlineRequiredReader) ReadInputRange(ctx context.Context, ref core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("parse context missing deadline")
	}
	return r.memoryArtifactReader.ReadInputRange(ctx, ref, offset, max)
}

func TestJestJSONParseAppliesDurationDeadlineToReader(t *testing.T) {
	base, ref := newArtifactReader(realFixture(t, "30.4.2"))
	base.input.RepositoryRoot = "/private/jest-fixture"
	reader := &deadlineRequiredReader{memoryArtifactReader: base}
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, jestLimits())
	if err != nil {
		t.Fatalf("parse did not propagate bounded context: %v", err)
	}
	if result.Outcome != core.ParseComplete {
		t.Fatalf("result=%#v", result)
	}
}
