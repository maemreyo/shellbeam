package jestjson

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func qualificationFixture(t *testing.T, version, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "jest-json", "jest-"+version, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseQualificationFixture(t *testing.T, version, name string) (result coreParseResult) {
	t.Helper()
	reader, ref := newArtifactReader(qualificationFixture(t, version, name))
	reader.input.RepositoryRoot = "/private/jest-fixture"
	limits := jestLimits()
	limits.MaxBytes = 16 << 20
	limits.MaxRecords = 8192
	limits.MaxDuration = 5 * time.Second
	parsed, err := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	return coreParseResult{Outcome: parsed.Outcome, Completeness: parsed.Completeness, Reason: parsed.CompletenessReason, Records: parsed.Records, Observed: parsed.ObservedEntries, Coverage: parsed.SemanticsCoverage}
}

type coreParseResult struct {
	Outcome      core.ParseOutcome
	Completeness core.Completeness
	Reason       core.CompletenessReason
	Records      []core.Record
	Observed     *core.ObservedEntryCounts
	Coverage     *core.ProducerSemanticsCoverage
}

func qualificationCases(result coreParseResult) []core.Record {
	var out []core.Record
	for _, record := range result.Records {
		if record.TestCase != nil {
			out = append(out, record)
		}
	}
	return out
}
func qualificationSuites(result coreParseResult) []core.Record {
	var out []core.Record
	for _, record := range result.Records {
		if record.TestSuite != nil {
			out = append(out, record)
		}
	}
	return out
}

func TestJestQualificationFixturesPinProducerSemantics(t *testing.T) {
	for _, version := range []string{"29.7.0", "30.4.2"} {
		t.Run(version, func(t *testing.T) {
			assertFixtureCaseStatus(t, version, "pass", core.TestPassed, 1, "")
			assertFixtureCaseStatus(t, version, "fail", core.TestFailed, 1, "")
			assertFixtureCaseStatus(t, version, "test-skip", core.TestSkipped, 1, "jest:pending")
			assertFixtureCaseStatus(t, version, "test-todo", core.TestSkipped, 1, "jest:todo")
			assertFixtureCaseStatus(t, version, "describe-skip", core.TestSkipped, 1, "jest:pending")
			assertFixtureCaseStatus(t, version, "before-all-throw", core.TestFailed, 1, "")
			assertFixtureCaseStatus(t, version, "before-each-throw", core.TestFailed, 1, "")
			assertFixtureCaseStatus(t, version, "after-all-throw", core.TestPassed, 1, "")
			assertFixtureCaseStatus(t, version, "retry-failed", core.TestFailed, 3, "")
			assertFixtureCaseStatus(t, version, "retry-passed", core.TestPassed, 3, "")

			module := parseQualificationFixture(t, version, "module-throw")
			if module.Outcome != core.ParseComplete || module.Completeness != core.CompletenessComplete || module.Observed == nil || module.Observed.Files != 1 || module.Observed.Entries != 0 || len(qualificationCases(module)) != 0 {
				t.Fatalf("module throw=%#v", module)
			}
			suites := qualificationSuites(module)
			if len(suites) != 1 || suites[0].TestSuite.Status != core.TestFailed {
				t.Fatalf("module suites=%#v", suites)
			}
			if module.Coverage == nil || module.Coverage.Family != "v29" {
				t.Fatalf("discriminator-free module coverage=%#v", module.Coverage)
			}

			focused := parseQualificationFixture(t, version, "focused-trap")
			focusedSuites := qualificationSuites(focused)
			focusedCases := qualificationCases(focused)
			if focused.Outcome != core.ParseComplete || len(focusedSuites) != 1 || len(focusedCases) != 2 || focusedSuites[0].TestSuite.Status != core.TestPassed || dispositionCode(focusedSuites[0].TestSuite.ProducerDisposition) != "jest:suite_focused" || focusedCases[0].TestCase.Status != core.TestPassed || focusedCases[1].TestCase.Status != core.TestSkipped {
				t.Fatalf("focused=%#v suites=%#v cases=%#v", focused, focusedSuites, focusedCases)
			}

			over := parseQualificationFixture(t, version, "over-cap")
			if over.Outcome != core.ParsePartial || over.Completeness != core.CompletenessPartial || over.Reason != core.CompletenessReasonPassRecordsElided || over.Observed == nil || over.Observed.Files != 1 || over.Observed.Entries != 8200 || len(over.Records) != 8192 {
				t.Fatalf("over-cap outcome=%s completeness=%s reason=%s observed=%#v records=%d", over.Outcome, over.Completeness, over.Reason, over.Observed, len(over.Records))
			}
			assertNoCoreErrorStatus(t, version, over)
		})
	}
}

func TestJestQualificationFixturesPinFailingProfileDifference(t *testing.T) {
	v29Expected := parseQualificationFixture(t, "29.7.0", "failing-expected")
	v29Unexpected := parseQualificationFixture(t, "29.7.0", "failing-unexpected")
	for name, result := range map[string]coreParseResult{"expected": v29Expected, "unexpected": v29Unexpected} {
		cases := qualificationCases(result)
		if result.Coverage == nil || result.Coverage.Family != "v29" || len(cases) != 1 || cases[0].TestCase.ProducerDisposition != nil {
			t.Fatalf("v29 %s=%#v cases=%#v", name, result.Coverage, cases)
		}
		if !containsString(result.Coverage.Unavailable, "jest:failing_expected") || !containsString(result.Coverage.Unavailable, "jest:failing_unexpected") {
			t.Fatalf("v29 %s unavailable=%v", name, result.Coverage.Unavailable)
		}
	}
	if cases := qualificationCases(v29Expected); cases[0].TestCase.Status != core.TestPassed {
		t.Fatalf("v29 expected=%#v", cases[0].TestCase)
	}
	if cases := qualificationCases(v29Unexpected); cases[0].TestCase.Status != core.TestFailed {
		t.Fatalf("v29 unexpected=%#v", cases[0].TestCase)
	}

	v30Expected := parseQualificationFixture(t, "30.4.2", "failing-expected")
	v30Unexpected := parseQualificationFixture(t, "30.4.2", "failing-unexpected")
	for _, tc := range []struct {
		name, code string
		result     coreParseResult
		status     core.TestStatus
	}{
		{"expected", "jest:failing_expected", v30Expected, core.TestPassed},
		{"unexpected", "jest:failing_unexpected", v30Unexpected, core.TestFailed},
	} {
		cases := qualificationCases(tc.result)
		if tc.result.Coverage == nil || tc.result.Coverage.Family != "v30" || len(cases) != 1 || cases[0].TestCase.Status != tc.status || dispositionCode(cases[0].TestCase.ProducerDisposition) != tc.code {
			t.Fatalf("v30 %s coverage=%#v cases=%#v", tc.name, tc.result.Coverage, cases)
		}
	}
}

func assertFixtureCaseStatus(t *testing.T, version, name string, status core.TestStatus, attempts int, disposition string) {
	t.Helper()
	result := parseQualificationFixture(t, version, name)
	cases := qualificationCases(result)
	if result.Outcome != core.ParseComplete || result.Completeness != core.CompletenessComplete || len(cases) != 1 || cases[0].TestCase.Status != status || cases[0].TestCase.AttemptCount == nil || *cases[0].TestCase.AttemptCount != attempts || dispositionCode(cases[0].TestCase.ProducerDisposition) != disposition {
		t.Fatalf("%s/%s result=%#v cases=%#v", version, name, result, cases)
	}
	assertNoCoreErrorStatus(t, version+"/"+name, result)
}

func assertNoCoreErrorStatus(t *testing.T, name string, result coreParseResult) {
	t.Helper()
	for _, record := range result.Records {
		if record.TestCase != nil && record.TestCase.Status == core.TestError {
			t.Fatalf("%s invented core error testcase: %#v", name, record.TestCase)
		}
		if record.TestSuite != nil && record.TestSuite.Status == core.TestError {
			t.Fatalf("%s invented core error suite: %#v", name, record.TestSuite)
		}
	}
}

type jestQualificationManifest struct {
	SchemaVersion int `json:"schema_version"`
	Fixtures      []struct {
		PackageVersion  string   `json:"package_version"`
		ProducerVersion string   `json:"producer_version"`
		Fixture         string   `json:"fixture"`
		GeneratorArgv   []string `json:"generator_argv"`
		ExitCode        int      `json:"exit_code"`
		SHA256          string   `json:"sha256"`
	} `json:"fixtures"`
}

func TestJestQualificationFixtureManifestPinsSHAAndProducerFacts(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "jest-json")
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest jestQualificationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Fixtures) != 30 {
		t.Fatalf("manifest schema=%d fixtures=%d", manifest.SchemaVersion, len(manifest.Fixtures))
	}
	seen := map[string]int{}
	for _, entry := range manifest.Fixtures {
		if len(entry.GeneratorArgv) != 5 || entry.GeneratorArgv[1] != "--runInBand" || entry.GeneratorArgv[2] != "--json" || !strings.HasPrefix(entry.GeneratorArgv[3], "--outputFile=") || entry.ProducerVersion == "" {
			t.Fatalf("invalid manifest entry=%#v", entry)
		}
		if entry.PackageVersion == "29.7.0" && entry.ProducerVersion != "29.7.0" {
			t.Fatalf("29 package producer=%q", entry.ProducerVersion)
		}
		if entry.PackageVersion == "30.4.2" && entry.ProducerVersion != "30.4.1" {
			t.Fatalf("30 package producer=%q", entry.ProducerVersion)
		}
		fixture, err := os.ReadFile(filepath.Join(root, entry.Fixture))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(fixture)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("fixture SHA drift: %s", entry.Fixture)
		}
		for _, forbidden := range []string{"/tmp/shellbeam-jest", "/Users/", "node_modules/jest/"} {
			if strings.Contains(string(fixture), forbidden) {
				t.Fatalf("fixture %s leaked generator/install path marker %q", entry.Fixture, forbidden)
			}
		}
		seen[entry.PackageVersion]++
	}
	if seen["29.7.0"] != 15 || seen["30.4.2"] != 15 {
		t.Fatalf("version fixture counts=%v", seen)
	}
}
