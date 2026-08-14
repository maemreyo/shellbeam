package gojson

import (
	"context"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestGoTestJSONMapsOnlyNativeTestAndSuiteFields(t *testing.T) {
	input := strings.Join([]string{
		`{"Time":"2026-08-14T09:00:00Z","Action":"run","Package":"example/a","Test":"TestPass"}`,
		`{"Time":"2026-08-14T09:00:00Z","Action":"pass","Package":"example/a","Test":"TestPass","Elapsed":0.01}`,
		`{"Time":"2026-08-14T09:00:00Z","Action":"fail","Package":"example/b","Test":"TestFail","Elapsed":0.02}`,
		`{"Time":"2026-08-14T09:00:00Z","Action":"skip","Package":"example/a","Test":"TestSkip","Elapsed":0}`,
		`{"Time":"2026-08-14T09:00:00Z","Action":"output","Package":"example/a","Test":"TestPass","Output":"=== RUN   TestPass\n"}`,
		`{"ImportPath":"example/build [example/build.test]","Action":"build-output","Output":"undefined: prose must not become a diagnostic\\n"}`,
		`{"Time":"2026-08-14T09:00:01Z","Action":"fail","Package":"example/build","Elapsed":0,"FailedBuild":"example/build [example/build.test]"}`,
		`{"Time":"2026-08-14T09:00:02Z","Action":"pass","Package":"example/a","Elapsed":0.03}`,
	}, "\n") + "\n"
	reader, ref := newMemoryReader(input)
	result, err := (TestAdapter{}).Parse(context.Background(), ref, reader, generousLimits())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParseComplete || result.Completeness != core.CompletenessComplete || len(result.Records) != 5 {
		t.Fatalf("result=%#v", result)
	}
	want := []struct {
		kind   core.RecordKind
		name   string
		status core.TestStatus
		pkg    string
	}{
		{core.RecordTestCase, "TestPass", core.TestPassed, "example/a"},
		{core.RecordTestCase, "TestFail", core.TestFailed, "example/b"},
		{core.RecordTestCase, "TestSkip", core.TestSkipped, "example/a"},
		{core.RecordTestSuite, "example/build", core.TestFailed, "example/build"},
		{core.RecordTestSuite, "example/a", core.TestPassed, "example/a"},
	}
	for i, expected := range want {
		record := result.Records[i]
		if record.Authority != core.AuthorityMechanical || record.DerivationMethod != core.DerivationNativeFieldMapping || record.RecordKind != expected.kind {
			t.Fatalf("record[%d]=%#v", i, record)
		}
		if record.TestCase != nil && (record.TestCase.Name != expected.name || record.TestCase.Status != expected.status || record.TestCase.Package != expected.pkg) {
			t.Fatalf("case[%d]=%#v", i, record.TestCase)
		}
		if record.TestSuite != nil && (record.TestSuite.Name != expected.name || record.TestSuite.Status != expected.status || record.TestSuite.Package != expected.pkg) {
			t.Fatalf("suite[%d]=%#v", i, record.TestSuite)
		}
	}
}

func TestGoTestJSONMalformedTruncatedAndBudgetOutcomesPreserveBoundedRecords(t *testing.T) {
	valid := `{"Action":"pass","Package":"example/a","Test":"TestOne","Elapsed":0.01}` + "\n"
	cases := []struct {
		name         string
		input        string
		limits       app.Limits
		outcome      core.ParseOutcome
		completeness core.Completeness
		records      int
	}{
		{"malformed", valid + `{"Action":@}`, generousLimits(), core.ParseMalformed, core.CompletenessPartial, 1},
		{"truncated", valid + `{"Action":"pass"`, generousLimits(), core.ParsePartial, core.CompletenessPartial, 1},
		{"oversized output string", valid + `{"Action":"output","Package":"example/a","Output":"` + strings.Repeat("x", 33) + `"}`, limitsWith(10, 32), core.ParseBudgetExceeded, core.CompletenessPartial, 1},
		{"record count", valid + valid + valid, limitsWith(2, 1024), core.ParseBudgetExceeded, core.CompletenessPartial, 2},
		{"byte budget", valid + valid, app.Limits{MaxBytes: int64(len(valid) + 4), MaxRecords: 10, MaxStringBytes: 1024, MaxDepth: 16, MaxDuration: time.Second}, core.ParseBudgetExceeded, core.CompletenessPartial, 1},
		{"depth budget", valid + `{"Action":"output","Package":"example/a","Extra":{"nested":{"too":"deep"}}}`, app.Limits{MaxBytes: 1 << 20, MaxRecords: 10, MaxStringBytes: 1024, MaxDepth: 2, MaxDuration: time.Second}, core.ParseBudgetExceeded, core.CompletenessPartial, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader, ref := newMemoryReader(tc.input)
			result, err := (TestAdapter{}).Parse(context.Background(), ref, reader, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.outcome || result.Completeness != tc.completeness || len(result.Records) != tc.records {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}
func TestGoTestJSONTimeBudgetIsEnforcedAfterBoundedRead(t *testing.T) {
	reader, ref := newMemoryReader(`{"Action":"pass","Package":"example/a","Test":"TestOne","Elapsed":0.01}` + "\n")
	reader.delay = 5 * time.Millisecond
	limits := generousLimits()
	limits.MaxDuration = time.Millisecond
	result, err := (TestAdapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParseBudgetExceeded || result.Completeness != core.CompletenessUnavailable || len(result.Records) != 0 {
		t.Fatalf("result=%#v", result)
	}
}
