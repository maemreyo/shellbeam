package structuredresult

import (
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

func TestSelectRecordsFailureFirstEverythingFits(t *testing.T) {
	input := []Record{
		budgetTestCase("pass-0", TestPassed),
		budgetTestCase("fail-1", TestFailed),
		budgetTestCase("skip-2", TestSkipped),
		budgetRecord("suite-3", RecordTestSuite),
		budgetRecord("diagnostic-4", RecordDiagnostic),
		budgetRecord("artifact-5", RecordArtifactResult),
	}

	got, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: len(input)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != ParseComplete || got.Completeness != CompletenessComplete || got.CompletenessReason != "" {
		t.Fatalf("outcome=%q completeness=%q reason=%q", got.Outcome, got.Completeness, got.CompletenessReason)
	}
	if !reflect.DeepEqual(got.Records, input) {
		t.Fatalf("records=%#v want=%#v", got.Records, input)
	}
}

func TestSelectRecordsFailureFirstElidesPassesButKeepsEveryMandatoryRecord(t *testing.T) {
	input := []Record{
		budgetTestCase("pass-0", TestPassed),
		budgetTestCase("fail-1", TestFailed),
		budgetTestCase("pass-2", TestPassed),
		budgetRecord("suite-3", RecordTestSuite),
		budgetRecord("diagnostic-4", RecordDiagnostic),
		budgetRecord("artifact-5", RecordArtifactResult),
		budgetTestCase("error-6", TestError),
	}

	got, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != ParsePartial || got.Completeness != CompletenessPartial || got.CompletenessReason != CompletenessReasonPassRecordsElided {
		t.Fatalf("outcome=%q completeness=%q reason=%q", got.Outcome, got.Completeness, got.CompletenessReason)
	}
	want := []string{"fail-1", "suite-3", "diagnostic-4", "artifact-5", "error-6"}
	if names := budgetRecordNames(got.Records); !reflect.DeepEqual(names, want) {
		t.Fatalf("records=%v want=%v", names, want)
	}
}

func TestSelectRecordsFailureFirstMandatoryOverflowIsBudgetExceeded(t *testing.T) {
	input := []Record{
		budgetTestCase("fail-0", TestFailed),
		budgetRecord("diagnostic-1", RecordDiagnostic),
		budgetTestCase("skip-2", TestSkipped),
		budgetRecord("suite-3", RecordTestSuite),
		budgetRecord("artifact-4", RecordArtifactResult),
	}

	got, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != ParseBudgetExceeded || got.Completeness != CompletenessPartial || got.CompletenessReason != "" {
		t.Fatalf("outcome=%q completeness=%q reason=%q", got.Outcome, got.Completeness, got.CompletenessReason)
	}
	want := []string{"fail-0", "diagnostic-1", "skip-2"}
	if names := budgetRecordNames(got.Records); !reflect.DeepEqual(names, want) {
		t.Fatalf("records=%v want=%v", names, want)
	}
}

func TestSelectRecordsFailureFirstPreservesDocumentOrderAfterMandatorySelection(t *testing.T) {
	input := []Record{
		budgetTestCase("pass-0", TestPassed),
		budgetTestCase("fail-1", TestFailed),
		budgetRecord("diagnostic-2", RecordDiagnostic),
		budgetRecord("suite-3", RecordTestSuite),
		budgetRecord("artifact-4", RecordArtifactResult),
		budgetTestCase("fail-5", TestFailed),
	}

	got, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fail-1", "diagnostic-2", "suite-3", "artifact-4", "fail-5"}
	if names := budgetRecordNames(got.Records); !reflect.DeepEqual(names, want) {
		t.Fatalf("emission order=%v want=%v", names, want)
	}
}

func TestSelectRecordsFailureFirstScaleIsStableAndRetainsEveryFailure(t *testing.T) {
	const (
		total = 10000
		fails = 50
		cap   = 8192
	)
	failPositions := map[int]bool{}
	rng := rand.New(rand.NewSource(42))
	for len(failPositions) < fails {
		failPositions[rng.Intn(total)] = true
	}

	input := make([]Record, 0, total)
	for i := 0; i < total; i++ {
		status := TestPassed
		if failPositions[i] {
			status = TestFailed
		}
		input = append(input, budgetTestCase(fmt.Sprintf("%05d", i), status))
	}

	first, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: cap})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SelectRecordsFailureFirst(input, RecordBudget{MaxRecords: cap})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("selection is not stable for identical input")
	}
	if len(first.Records) != cap || first.Outcome != ParsePartial || first.Completeness != CompletenessPartial || first.CompletenessReason != CompletenessReasonPassRecordsElided {
		t.Fatalf("selection len=%d outcome=%q completeness=%q reason=%q", len(first.Records), first.Outcome, first.Completeness, first.CompletenessReason)
	}

	selectedFailures := map[int]bool{}
	previous := -1
	for _, record := range first.Records {
		idx, err := strconv.Atoi(record.TestCase.Name)
		if err != nil {
			t.Fatal(err)
		}
		if idx <= previous {
			t.Fatalf("records not emitted in document order: %d after %d", idx, previous)
		}
		previous = idx
		if record.TestCase.Status == TestFailed {
			selectedFailures[idx] = true
		}
	}
	if !reflect.DeepEqual(selectedFailures, failPositions) {
		t.Fatalf("selected failures=%v want=%v", selectedFailures, failPositions)
	}
}

func TestSelectRecordsFailureFirstRejectsInvalidBudget(t *testing.T) {
	for _, max := range []int{0, -1} {
		if _, err := SelectRecordsFailureFirst(nil, RecordBudget{MaxRecords: max}); err == nil {
			t.Fatalf("MaxRecords=%d accepted", max)
		}
	}
}

func budgetTestCase(name string, status TestStatus) Record {
	return Record{RecordKind: RecordTestCase, TestCase: &TestCase{Name: name, Status: status}}
}

func budgetRecord(name string, kind RecordKind) Record {
	record := Record{RecordKind: kind}
	switch kind {
	case RecordDiagnostic:
		record.Diagnostic = &Diagnostic{Message: name}
	case RecordTestSuite:
		record.TestSuite = &TestSuite{Name: name, Status: TestPassed}
	case RecordArtifactResult:
		record.ArtifactResult = &ArtifactResult{Name: name, Status: "ok"}
	}
	return record
}

func budgetRecordNames(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		switch record.RecordKind {
		case RecordTestCase:
			out = append(out, record.TestCase.Name)
		case RecordDiagnostic:
			out = append(out, record.Diagnostic.Message)
		case RecordTestSuite:
			out = append(out, record.TestSuite.Name)
		case RecordArtifactResult:
			out = append(out, record.ArtifactResult.Name)
		}
	}
	return out
}
