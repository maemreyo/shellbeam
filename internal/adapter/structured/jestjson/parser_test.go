package jestjson

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type memoryArtifactReader struct {
	data  []byte
	ref   core.StructuredInputRef
	input app.InputContext
}

func newArtifactReader(data []byte) (*memoryArtifactReader, core.StructuredInputRef) {
	blob := core.ArtifactBlobRef{
		SchemaVersion:           core.ArtifactBlobSchemaVersion,
		BlobID:                  "abl_" + strings.Repeat("a", 64),
		OperationID:             "jest-parser-op",
		SessionID:               "jest-parser-session",
		RepositoryID:            "repo_01M09A27JCSE71BXSP477EKN34",
		WorkspaceID:             "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		DeclaredPath:            "reports/jest.json",
		NormalizedWorkspacePath: "reports/jest.json",
		SHA256:                  strings.Repeat("b", 64),
		Size:                    int64(len(data)),
		TerminalCut:             core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 2, ReceiptDigest: strings.Repeat("c", 64)},
		ObservationCut:          core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("d", 64)},
	}
	ref := core.ArtifactInputRef(blob)
	return &memoryArtifactReader{
		data:  append([]byte(nil), data...),
		ref:   ref,
		input: app.InputContext{OperationID: blob.OperationID, DerivationKey: strings.Repeat("e", 64), RepositoryRoot: "/repo"},
	}, ref
}

func (r *memoryArtifactReader) ReadInputRange(_ context.Context, ref core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	if ref.Validate() != nil || ref.ArtifactBlob == nil || ref.ArtifactBlob.BlobID != r.ref.ArtifactBlob.BlobID || offset < 0 || max < 0 || offset > int64(len(r.data)) {
		return nil, errors.New("invalid artifact range")
	}
	end := offset + int64(max)
	if end > int64(len(r.data)) {
		end = int64(len(r.data))
	}
	return append([]byte(nil), r.data[offset:end]...), nil
}

func (r *memoryArtifactReader) DescribeInput(context.Context, core.StructuredInputRef) (app.InputContext, error) {
	return r.input, nil
}

func jestLimits() app.Limits {
	return app.Limits{MaxBytes: 1 << 20, MaxRecords: 4096, MaxStringBytes: 64 << 10, MaxDepth: 64, MaxDuration: time.Second}
}

func parseBytes(t *testing.T, data []byte) app.ParseResult {
	t.Helper()
	return parseBytesWithRoot(t, data, "/repo")
}

func parseBytesWithRoot(t *testing.T, data []byte, root string) app.ParseResult {
	t.Helper()
	reader, ref := newArtifactReader(data)
	reader.input.RepositoryRoot = root
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, jestLimits())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func parseRealFixture(t *testing.T, version string) app.ParseResult {
	t.Helper()
	return parseBytesWithRoot(t, realFixture(t, version), "/private/jest-fixture")
}

func realFixture(t *testing.T, version string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "jest-json", "real-doc-fixtures", "jest-"+version, "pass.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestJestJSONDiscriminatesFrozenV29AndV30Profiles(t *testing.T) {
	cases := []struct {
		version string
		family  string
	}{{"29.7.0", "v29"}, {"30.4.2", "v30"}}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			result := parseRealFixture(t, tc.version)
			if result.Outcome != core.ParseComplete || result.Completeness != core.CompletenessComplete {
				t.Fatalf("result=%#v", result)
			}
			if result.SemanticsCoverage == nil || result.SemanticsCoverage.Family != tc.family {
				t.Fatalf("coverage=%#v", result.SemanticsCoverage)
			}
		})
	}
}

func TestJestJSONProfileGateFailsClosed(t *testing.T) {
	valid := realFixture(t, "30.4.2")
	cases := []struct {
		name    string
		data    []byte
		outcome core.ParseOutcome
	}{
		{"unknown member", append([]byte(`{"futureMember":true,`), valid[1:]...), core.ParseUnavailable},
		{"duplicate member", append([]byte(`{"success":false,`), valid[1:]...), core.ParseMalformed},
		{"listTests payload", []byte(`["/repo/a.test.js"]`), core.ParseUnavailable},
		{"trailing bytes", append(append([]byte(nil), valid...), []byte(`{}`)...), core.ParseMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBytes(t, tc.data)
			if result.Outcome != tc.outcome || result.Completeness != core.CompletenessUnavailable || len(result.Records) != 0 {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestJestJSONPersistsStructuralIdentityAddressAndDuration(t *testing.T) {
	f := false
	start := int64(0)
	duration := 12.987
	assertions := []assertionV30{
		{AncestorTitles: []string{"outer", "inner"}, Duration: &duration, FullName: "ambiguous full name must not drive identity", Title: "duplicate name", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1},
		{AncestorTitles: []string{"outer", "inner"}, Duration: &duration, FullName: "same producer fullName", Title: "duplicate name", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1},
	}
	result := parseBytes(t, syntheticV30(t, "passed", assertions))
	cases := jestTestcaseRecords(result)
	if result.Outcome != core.ParseComplete || len(cases) != 2 {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	for i, record := range cases {
		entry := record.TestCase.ArtifactEntry
		address := record.TestCase.ProducerAddress
		if entry == nil || entry.ArtifactBlobID != record.SourceRef.ArtifactBlob.BlobID || entry.SuiteOrdinal != 0 || entry.TestcaseOrdinal != i {
			t.Fatalf("case[%d] entry=%#v", i, entry)
		}
		wantID, err := core.ArtifactTestRecordID(strings.Repeat("e", 64), *entry)
		if err != nil {
			t.Fatal(err)
		}
		if record.RecordID != wantID {
			t.Fatalf("case[%d] record_id=%q want=%q", i, record.RecordID, wantID)
		}
		if address == nil || address.Namespace != "jest" || address.VocabularyVersion != 1 || address.SuiteName != "src/a.test.js" || address.Classname != "outer > inner" || address.Name != "duplicate name" {
			t.Fatalf("case[%d] address=%#v", i, address)
		}
		if record.TestCase.DurationMS != 12 {
			t.Fatalf("case[%d] duration_ms=%d", i, record.TestCase.DurationMS)
		}
		if record.TestCase.Name != "duplicate name" || strings.Contains(record.TestCase.Name, "fullName") {
			t.Fatalf("case[%d] name=%q", i, record.TestCase.Name)
		}
		if err := record.Validate(); err != nil {
			t.Fatalf("case[%d] invalid: %v", i, err)
		}
	}
	if cases[0].RecordID == cases[1].RecordID {
		t.Fatalf("duplicate producer addresses collapsed to one record id: %q", cases[0].RecordID)
	}
	suites := jestSuiteRecords(result)
	if len(suites) != 1 || suites[0].TestSuite.Name != "src/a.test.js" || suites[0].TestSuite.DurationMS != 0 {
		t.Fatalf("suite=%#v", suites)
	}
}

func TestJestJSONRedactsNonRepositoryFilePathsWithoutPersistingHostPath(t *testing.T) {
	f := false
	start := int64(0)
	assertion := assertionV30{Title: "pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}
	for _, tc := range []struct {
		name, path, marker string
	}{
		{"external", "/Users/alice/private/secret.test.js", "workspace_external_redacted"},
		{"system", "/usr/lib/system.test.js", "system_classified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := documentV30{OpenHandles: []byte(`[]`), Snapshot: []byte(`{}`), Success: true, TestResults: []testResultV30{{AssertionResults: []assertionV30{assertion}, Name: tc.path, Status: "passed"}}}
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			result := parseBytes(t, data)
			if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial {
				t.Fatalf("result=%#v", result)
			}
			encoded, err := json.Marshal(result.Records)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), tc.path) || !strings.Contains(string(encoded), tc.marker) {
				t.Fatalf("unsafe path persistence: %s", encoded)
			}
			cases := jestTestcaseRecords(result)
			if len(cases) != 1 || cases[0].TestCase.ProducerAddress != nil {
				t.Fatalf("external/system testcase address should be unavailable: %#v", cases)
			}
		})
	}
}

func TestJestJSONNegativeDurationIsUnavailableAndMarksPartial(t *testing.T) {
	f := false
	start := int64(0)
	duration := -1.5
	assertion := assertionV30{Title: "negative duration", Duration: &duration, Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}
	result := parseBytes(t, syntheticV30(t, "passed", []assertionV30{assertion}))
	cases := jestTestcaseRecords(result)
	if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial || len(cases) != 1 || cases[0].TestCase.DurationMS != 0 {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
}

func TestJestJSONPopulatesBoundedFailureExcerptOnlyForNonPass(t *testing.T) {
	f := false
	start := int64(0)
	fail := assertionV30{
		Title: "fail", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1,
		FailureMessages: []string{"\x1b[31mboom\x1b[0m at /repo/src/a.test.js:12:3", "second failure must not be persisted"},
	}
	pass := assertionV30{
		Title: "pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1,
		FailureMessages: []string{"pass prose must not be persisted"},
	}
	result := parseBytes(t, syntheticV30(t, "failed", []assertionV30{fail, pass}))
	cases := jestTestcaseRecords(result)
	if result.Outcome != core.ParseComplete || len(cases) != 2 {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	got := cases[0]
	if got.SchemaVersion != core.RecordSchemaVersionV3 || got.TestCase.FailureExcerpt == nil {
		t.Fatalf("failed record missing v3 excerpt: %#v", got)
	}
	if excerpt := got.TestCase.FailureExcerpt; excerpt.Namespace != "jest" || excerpt.VocabularyVersion != 1 || excerpt.Text != "boom at src/a.test.js:12:3" || excerpt.Truncated || excerpt.Redacted {
		t.Fatalf("excerpt=%#v", excerpt)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "second failure") || strings.Contains(string(encoded), "\\u001b") {
		t.Fatalf("unsafe/extra failure prose persisted: %s", encoded)
	}
	if cases[1].SchemaVersion != core.SchemaVersion || cases[1].TestCase.FailureExcerpt != nil {
		t.Fatalf("pass record received excerpt: %#v", cases[1])
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("v3 failed record invalid: %v", err)
	}
}

func TestJestJSONExcerptNormalizationFailureOmitsExcerptAndMarksPartial(t *testing.T) {
	f := false
	start := int64(0)
	assertion := assertionV30{
		Title: "fail", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1,
		FailureMessages: []string{"\x1b[31m"},
	}
	result := parseBytes(t, syntheticV30(t, "failed", []assertionV30{assertion}))
	cases := jestTestcaseRecords(result)
	if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial || len(cases) != 1 {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	if cases[0].SchemaVersion != core.SchemaVersion || cases[0].TestCase.Status != core.TestFailed || cases[0].TestCase.FailureExcerpt != nil {
		t.Fatalf("unsafe excerpt affected status or schema: %#v", cases[0])
	}
}

func TestJestJSONFailureExcerptDoesNotChangeStructuralRecordIdentity(t *testing.T) {
	f := false
	start := int64(0)
	base := assertionV30{Title: "same", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1}
	without := jestTestcaseRecords(parseBytes(t, syntheticV30(t, "failed", []assertionV30{base})))
	base.FailureMessages = []string{"bounded failure"}
	with := jestTestcaseRecords(parseBytes(t, syntheticV30(t, "failed", []assertionV30{base})))
	if len(without) != 1 || len(with) != 1 || without[0].RecordID == "" || without[0].RecordID != with[0].RecordID {
		t.Fatalf("record identity changed: without=%#v with=%#v", without, with)
	}
}

func TestJestJSONMapsAssertionStatusDispositionsAndAttempts(t *testing.T) {
	f := false
	tr := true
	start := int64(0)
	assertions := []assertionV30{
		{Title: "pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1},
		{Title: "expected failure", Status: "passed", Failing: &tr, StartAt: &start, Invocations: 1},
		{Title: "unexpected pass", Status: "failed", Failing: &tr, StartAt: &start, Invocations: 1},
		{Title: "fail", Status: "failed", Failing: &f, StartAt: &start, Invocations: 1},
		{Title: "pending", Status: "pending", Failing: &f, StartAt: &start, Invocations: 1},
		{Title: "todo", Status: "todo", Failing: &f, StartAt: &start, Invocations: 1},
		{Title: "retried pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 3},
	}
	result := parseBytes(t, syntheticV30(t, "failed", assertions))
	cases := jestTestcaseRecords(result)
	wantStatus := []core.TestStatus{core.TestPassed, core.TestPassed, core.TestFailed, core.TestFailed, core.TestSkipped, core.TestSkipped, core.TestPassed}
	wantCode := []string{"", "jest:failing_expected", "jest:failing_unexpected", "", "jest:pending", "jest:todo", ""}
	if result.Outcome != core.ParseComplete || len(cases) != len(wantStatus) {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	for i := range wantStatus {
		if cases[i].TestCase.Status != wantStatus[i] || dispositionCode(cases[i].TestCase.ProducerDisposition) != wantCode[i] {
			t.Fatalf("case[%d]=%#v", i, cases[i].TestCase)
		}
	}
	if got := cases[6].TestCase.AttemptCount; got == nil || *got != 3 || cases[6].TestCase.Status != core.TestPassed {
		t.Fatalf("retried pass=%#v", cases[6].TestCase)
	}
}

func TestJestJSONMapsFileStatusFromProducerWithoutRecomputation(t *testing.T) {
	f := false
	start := int64(0)
	pass := assertionV30{Title: "pass", Status: "passed", Failing: &f, StartAt: &start, Invocations: 1}
	pending := assertionV30{Title: "pending", Status: "pending", Failing: &f, StartAt: &start, Invocations: 1}
	cases := []struct {
		name, fileStatus string
		assertions       []assertionV30
		want             core.TestStatus
		code             string
	}{
		{"failed", "failed", []assertionV30{pass}, core.TestFailed, ""},
		{"passed", "passed", []assertionV30{pass}, core.TestPassed, ""},
		{"skipped", "skipped", []assertionV30{pass}, core.TestSkipped, ""},
		{"focused", "focused", []assertionV30{pass, pending}, core.TestPassed, "jest:suite_focused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseBytes(t, syntheticV30(t, tc.fileStatus, tc.assertions))
			suites := jestSuiteRecords(result)
			if result.Outcome != core.ParseComplete || len(suites) != 1 || suites[0].TestSuite.Status != tc.want || dispositionCode(suites[0].TestSuite.ProducerDisposition) != tc.code {
				t.Fatalf("result=%#v suites=%#v", result, suites)
			}
		})
	}
}

func TestJestJSONV29KeepsFailingSemanticsUnavailable(t *testing.T) {
	result := parseBytes(t, syntheticV29(t, "passed", []assertionV29{{Title: "pass", Status: "passed", Invocations: 1}}))
	cases := jestTestcaseRecords(result)
	if result.Outcome != core.ParseComplete || len(cases) != 1 || cases[0].TestCase.Status != core.TestPassed || cases[0].TestCase.ProducerDisposition != nil {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	coverage := result.SemanticsCoverage
	if coverage == nil || !containsString(coverage.Unavailable, "jest:failing_expected") || !containsString(coverage.Unavailable, "jest:failing_unexpected") {
		t.Fatalf("coverage=%#v", coverage)
	}
}

func TestJestJSONRejectsUnsupportedAssertionStatus(t *testing.T) {
	f := false
	start := int64(0)
	for _, status := range []string{"skipped", "disabled", "focused"} {
		t.Run(status, func(t *testing.T) {
			data := syntheticV30(t, "passed", []assertionV30{{Title: "future", Status: status, Failing: &f, StartAt: &start, Invocations: 1}})
			result := parseBytes(t, data)
			if result.Outcome != core.ParseUnavailable || result.Completeness != core.CompletenessUnavailable || len(result.Records) != 0 {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func syntheticV30(t *testing.T, fileStatus string, assertions []assertionV30) []byte {
	t.Helper()
	doc := documentV30{OpenHandles: []byte(`[]`), Snapshot: []byte(`{}`), Success: true, TestResults: []testResultV30{{AssertionResults: assertions, Name: "/repo/src/a.test.js", Status: fileStatus}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func syntheticV29(t *testing.T, fileStatus string, assertions []assertionV29) []byte {
	t.Helper()
	doc := documentV29{OpenHandles: []byte(`[]`), Snapshot: []byte(`{}`), Success: true, TestResults: []testResultV29{{AssertionResults: assertions, Name: "/repo/src/a.test.js", Status: fileStatus}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jestTestcaseRecords(result app.ParseResult) []core.Record {
	out := make([]core.Record, 0)
	for _, record := range result.Records {
		if record.RecordKind == core.RecordTestCase {
			out = append(out, record)
		}
	}
	return out
}

func jestSuiteRecords(result app.ParseResult) []core.Record {
	out := make([]core.Record, 0)
	for _, record := range result.Records {
		if record.RecordKind == core.RecordTestSuite {
			out = append(out, record)
		}
	}
	return out
}

func dispositionCode(value *core.ProducerTestDisposition) string {
	if value == nil {
		return ""
	}
	return value.Code
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
