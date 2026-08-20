package pytestjunit

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

type memoryArtifactReader struct {
	data  []byte
	ref   core.StructuredInputRef
	input app.InputContext
}

func newArtifactReader(text string) (*memoryArtifactReader, core.StructuredInputRef) {
	blob := core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: "abl_" + strings.Repeat("a", 64),
		OperationID: "pytest-parser-op", SessionID: "pytest-parser-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		DeclaredPath: "reports/junit.xml", NormalizedWorkspacePath: "reports/junit.xml",
		SHA256: strings.Repeat("b", 64), Size: int64(len(text)),
		TerminalCut:    core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 2, ReceiptDigest: strings.Repeat("c", 64)},
		ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("d", 64)},
	}
	ref := core.ArtifactInputRef(blob)
	return &memoryArtifactReader{data: []byte(text), ref: ref, input: app.InputContext{OperationID: blob.OperationID, DerivationKey: strings.Repeat("e", 64)}}, ref
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

func pytestLimits() app.Limits {
	return app.Limits{MaxBytes: 1 << 20, MaxRecords: 2048, MaxStringBytes: 64 << 10, MaxDepth: 64, MaxDuration: time.Second}
}

func parseFixture(t *testing.T, xml string) app.ParseResult {
	t.Helper()
	reader, ref := newArtifactReader(xml)
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, pytestLimits())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testcaseRecords(result app.ParseResult) []core.Record {
	out := make([]core.Record, 0)
	for _, record := range result.Records {
		if record.RecordKind == core.RecordTestCase {
			out = append(out, record)
		}
	}
	return out
}
func suiteRecords(result app.ParseResult) []core.Record {
	out := make([]core.Record, 0)
	for _, record := range result.Records {
		if record.RecordKind == core.RecordTestSuite {
			out = append(out, record)
		}
	}
	return out
}

func TestPytestJUnitMapsOnlyMechanicallyObservableOutcomes(t *testing.T) {
	xml := `<testsuites><testsuite name="pytest" tests="7" failures="2" errors="1" skipped="2" time="0.070">` +
		`<testcase classname="pkg.TestA" name="test_pass" time="0.001"/>` +
		`<testcase classname="pkg.TestA" name="test_fail" time="0.002"><failure message="assert 1 == 2">ordinary failure prose</failure></testcase>` +
		`<testcase classname="pkg.TestA" name="test_error" time="0.003"><error message="teardown exploded">phase-looking prose</error></testcase>` +
		`<testcase classname="pkg.TestA" name="test_skip" time="0.004"><skipped type="pytest.skip" message="reason text"/></testcase>` +
		`<testcase classname="pkg.TestA" name="test_xfail" time="0.005"><skipped type="pytest.xfail" message="expected failure"/></testcase>` +
		`<testcase classname="pkg.TestA" name="test_non_strict_xpass" time="0.006"/>` +
		`<testcase classname="pkg.TestA" name="test_strict_xpass" time="0.007"><failure message="[XPASS(strict)] must fail">XPASS prose MUST NOT become disposition</failure></testcase>` +
		`</testsuite></testsuites>`
	result := parseFixture(t, xml)
	if result.Outcome != core.ParseComplete || result.Completeness != core.CompletenessComplete {
		t.Fatalf("result=%#v", result)
	}
	cases := testcaseRecords(result)
	want := []core.TestStatus{core.TestPassed, core.TestFailed, core.TestError, core.TestSkipped, core.TestSkipped, core.TestPassed, core.TestFailed}
	if len(cases) != len(want) {
		t.Fatalf("cases=%d records=%#v", len(cases), result.Records)
	}
	for i, status := range want {
		if cases[i].TestCase.Status != status {
			t.Fatalf("case[%d]=%#v", i, cases[i].TestCase)
		}
		if err := cases[i].Validate(); err != nil {
			t.Fatalf("record[%d] invalid: %v", i, err)
		}
	}
	if got := cases[3].TestCase.ProducerDisposition; got == nil || got.Code != "pytest:skip" {
		t.Fatalf("skip disposition=%#v", got)
	}
	if got := cases[4].TestCase.ProducerDisposition; got == nil || got.Code != "pytest:xfail" {
		t.Fatalf("xfail disposition=%#v", got)
	}
	for _, i := range []int{0, 1, 2, 5, 6} {
		if cases[i].TestCase.ProducerDisposition != nil {
			t.Fatalf("case[%d] invented disposition=%#v", i, cases[i].TestCase.ProducerDisposition)
		}
	}
	encoded, _ := json.Marshal(cases[6])
	if strings.Contains(string(encoded), "XPASS(strict)") || strings.Contains(string(encoded), "must fail") {
		t.Fatalf("producer prose leaked into normalized record: %s", encoded)
	}
	coverage := result.SemanticsCoverage
	if coverage == nil || coverage.Namespace != "pytest" || coverage.Format != "junit-xml" || coverage.Family != "xunit2" {
		t.Fatalf("coverage=%#v", coverage)
	}
	wantObservable := []string{"coarse:error", "coarse:fail", "coarse:pass", "coarse:skip", "pytest:skip", "pytest:xfail"}
	wantUnavailable := []string{"pytest:error_phase", "pytest:xfail_execution_state", "pytest:xpass_exact"}
	if strings.Join(coverage.MechanicallyObservable, ",") != strings.Join(wantObservable, ",") || strings.Join(coverage.Unavailable, ",") != strings.Join(wantUnavailable, ",") {
		t.Fatalf("coverage=%#v", coverage)
	}
}

func TestPytestJUnitUnknownSkippedSubtypeIsCoarseSkipAndPartial(t *testing.T) {
	result := parseFixture(t, `<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="1"><testcase classname="pkg" name="test_skip"><skipped type="vendor.special" message="opaque prose"/></testcase></testsuite>`)
	cases := testcaseRecords(result)
	if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial || len(cases) != 1 || cases[0].TestCase.Status != core.TestSkipped || cases[0].TestCase.ProducerDisposition != nil {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
	if result.Summary.Warnings != 1 {
		t.Fatalf("summary=%#v", result.Summary)
	}
	var diagnostic *core.Diagnostic
	for _, record := range result.Records {
		if record.Diagnostic != nil {
			diagnostic = record.Diagnostic
		}
	}
	if diagnostic == nil || diagnostic.Code != "pytest_skipped_subtype_unavailable" || strings.Contains(diagnostic.Message, "opaque prose") {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
}

func TestPytestJUnitPreservesDuplicateAddressesAndMultiEntryShape(t *testing.T) {
	result := parseFixture(t, `<testsuites><testsuite name="pytest" tests="4" failures="1" errors="1" skipped="0">`+
		`<testcase classname="pkg.C" name="test_dup"/><testcase classname="pkg.C" name="test_dup"/>`+
		`<testcase classname="pkg.C" name="test_multi"><failure message="call"/></testcase>`+
		`<testcase classname="pkg.C" name="test_multi"><error message="teardown"/></testcase>`+
		`</testsuite></testsuites>`)
	cases := testcaseRecords(result)
	if result.Outcome != core.ParseComplete || len(cases) != 4 {
		t.Fatalf("result=%#v", result)
	}
	seen := map[string]bool{}
	for i, record := range cases {
		entry := record.TestCase.ArtifactEntry
		address := record.TestCase.ProducerAddress
		if entry == nil || entry.SuiteOrdinal != 0 || entry.TestcaseOrdinal != i || address == nil || address.Classname != "pkg.C" {
			t.Fatalf("case[%d]=%#v", i, record.TestCase)
		}
		if seen[record.RecordID] {
			t.Fatalf("duplicate record id=%s", record.RecordID)
		}
		seen[record.RecordID] = true
	}
	if cases[0].TestCase.ProducerAddress.Name != cases[1].TestCase.ProducerAddress.Name || cases[0].RecordID == cases[1].RecordID {
		t.Fatalf("duplicate producer addresses collapsed: %#v %#v", cases[0], cases[1])
	}
	if cases[2].TestCase.ProducerAddress.Name != cases[3].TestCase.ProducerAddress.Name || cases[2].TestCase.Status != core.TestFailed || cases[3].TestCase.Status != core.TestError {
		t.Fatalf("multi-entry shape lost: %#v %#v", cases[2], cases[3])
	}
}

func TestPytestJUnitSuiteAggregateIsProducerAuthorityNotRecomputed(t *testing.T) {
	result := parseFixture(t, `<testsuite name="producer-suite" tests="3" failures="1" errors="1" skipped="1" time="1.250">`+
		`<testcase classname="pkg" name="a"/><testcase classname="pkg" name="b"/><testcase classname="pkg" name="c"/>`+
		`</testsuite>`)
	suites := suiteRecords(result)
	if len(suites) != 1 {
		t.Fatalf("suites=%#v", suites)
	}
	suite := suites[0].TestSuite
	if suite.Status != core.TestError || suite.DurationMS != 1250 || suite.Aggregate == nil || *suite.Aggregate != (core.TestSuiteAggregate{Tests: 3, Failures: 1, Errors: 1, Skipped: 1}) {
		t.Fatalf("suite=%#v", suite)
	}
	cases := testcaseRecords(result)
	for _, record := range cases {
		if record.TestCase.Status != core.TestPassed {
			t.Fatalf("suite counters rewrote child=%#v", record.TestCase)
		}
	}
}

func TestPytestJUnitUnsupportedIndependentTestcaseExtensionKeepsMechanicalRecordPartial(t *testing.T) {
	result := parseFixture(t, `<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="0"><testcase classname="pkg" name="a"><vendor-extension answer="42"/></testcase></testsuite>`)
	cases := testcaseRecords(result)
	if result.Outcome != core.ParsePartial || result.Completeness != core.CompletenessPartial || len(cases) != 1 || cases[0].Authority != core.AuthorityMechanical || cases[0].TestCase.Status != core.TestPassed {
		t.Fatalf("result=%#v cases=%#v", result, cases)
	}
}
