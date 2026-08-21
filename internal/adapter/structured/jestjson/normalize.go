package jestjson

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/inputtrace"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func normalizeProfile(ctx context.Context, profile observedProfile, ref core.StructuredInputRef, input app.InputContext) (app.ParseResult, bool, error) {
	producer := core.Producer{AdapterID: (Adapter{}).ID(), AdapterVersion: adapterVersion, CapabilityVersion: capabilityVersion}
	result := app.ParseResult{Outcome: core.ParseComplete, Completeness: core.CompletenessComplete, SemanticsCoverage: semanticsCoverage(profile.family)}
	var records []core.Record
	var partial, ok bool
	if profile.v30 != nil {
		records, partial, ok = normalizeV30(ctx, *profile.v30, ref, input, producer)
	} else if profile.v29 != nil {
		records, partial, ok = normalizeV29(ctx, *profile.v29, ref, input, producer)
	}
	if err := ctx.Err(); err != nil {
		return app.ParseResult{}, false, err
	}
	if !ok {
		return app.ParseResult{Outcome: core.ParseUnavailable, Completeness: core.CompletenessUnavailable}, false, nil
	}
	result.Records = records
	files := 0
	if profile.v30 != nil {
		files = len(profile.v30.TestResults)
	} else if profile.v29 != nil {
		files = len(profile.v29.TestResults)
	}
	result.ObservedEntries = observedEntries(result.Records, files)
	result.Summary = summarizeRecords(result.Records)
	if partial {
		result.Outcome = core.ParsePartial
		result.Completeness = core.CompletenessPartial
	}
	return result, true, nil
}

func observedEntries(records []core.Record, files int) *core.ObservedEntryCounts {
	counts := &core.ObservedEntryCounts{Namespace: "jest", VocabularyVersion: jestVocabularyV1, Files: files}
	for _, record := range records {
		if record.TestCase == nil {
			continue
		}
		counts.Entries++
		switch record.TestCase.Status {
		case core.TestPassed:
			counts.Pass++
		case core.TestFailed:
			counts.Fail++
		case core.TestSkipped:
			counts.Skip++
		case core.TestError:
			counts.Error++
		}
	}
	return counts
}

func normalizeV30(ctx context.Context, doc documentV30, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) ([]core.Record, bool, bool) {
	records := make([]core.Record, 0)
	partial := false
	for fileIndex, file := range doc.TestResults {
		if checkJestContext(ctx, fileIndex) != nil {
			return nil, false, false
		}
		pathInfo, ok := classifyJestFilePath(file.Name, input.RepositoryRoot)
		if !ok {
			return nil, false, false
		}
		partial = partial || pathInfo.partial
		suite, ok := normalizeSuiteRecord(pathInfo.persistedName, file.Status, ref, input, producer)
		if !ok {
			return nil, false, false
		}
		records = append(records, suite)
		for assertionIndex, assertion := range file.AssertionResults {
			if checkJestContext(ctx, assertionIndex) != nil {
				return nil, false, false
			}
			record, recordPartial, ok := normalizeAssertionV30(assertion, fileIndex, assertionIndex, pathInfo, ref, input, producer)
			if !ok {
				return nil, false, false
			}
			partial = partial || recordPartial
			records = append(records, record)
		}
	}
	return records, partial, true
}

func normalizeV29(ctx context.Context, doc documentV29, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) ([]core.Record, bool, bool) {
	records := make([]core.Record, 0)
	partial := false
	for fileIndex, file := range doc.TestResults {
		if checkJestContext(ctx, fileIndex) != nil {
			return nil, false, false
		}
		pathInfo, ok := classifyJestFilePath(file.Name, input.RepositoryRoot)
		if !ok {
			return nil, false, false
		}
		partial = partial || pathInfo.partial
		suite, ok := normalizeSuiteRecord(pathInfo.persistedName, file.Status, ref, input, producer)
		if !ok {
			return nil, false, false
		}
		records = append(records, suite)
		for assertionIndex, assertion := range file.AssertionResults {
			if checkJestContext(ctx, assertionIndex) != nil {
				return nil, false, false
			}
			record, recordPartial, ok := normalizeAssertionV29(assertion, fileIndex, assertionIndex, pathInfo, ref, input, producer)
			if !ok {
				return nil, false, false
			}
			partial = partial || recordPartial
			records = append(records, record)
		}
	}
	return records, partial, true
}

type jestFilePath struct {
	persistedName string
	suiteName     string
	repoRelative  bool
	partial       bool
}

func classifyJestFilePath(value, workspaceRoot string) (jestFilePath, bool) {
	root := filepath.Clean(workspaceRoot)
	clean := filepath.Clean(value)
	if !filepath.IsAbs(root) || !filepath.IsAbs(clean) {
		return jestFilePath{}, false
	}
	if rel, err := filepath.Rel(root, clean); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.ToSlash(rel)
		return jestFilePath{persistedName: rel, suiteName: rel, repoRelative: true}, true
	}
	if class := jestSystemPathClass(clean); class != "" {
		marker := "[" + string(inputtrace.PathSystemClassified) + ":" + class + "]"
		return jestFilePath{persistedName: marker, partial: true}, true
	}
	return jestFilePath{persistedName: "[" + string(inputtrace.PathWorkspaceExternalRedacted) + "]", partial: true}, true
}

func jestSystemPathClass(value string) string {
	for _, item := range []struct{ prefix, class string }{
		{"/usr", "usr"}, {"/System", "system"}, {"/Library", "library"}, {"/bin", "system"}, {"/sbin", "system"}, {"/dev", "device"},
	} {
		if value == item.prefix || strings.HasPrefix(value, item.prefix+string(filepath.Separator)) {
			return item.class
		}
	}
	return ""
}

func normalizeAssertionV30(assertion assertionV30, suiteOrdinal, testcaseOrdinal int, pathInfo jestFilePath, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) (core.Record, bool, bool) {
	status, disposition, ok := mapAssertionStatus(assertion.Status, assertion.Failing)
	if !ok || assertion.Invocations < 1 || assertion.Invocations > 1<<20 || assertion.Title == "" {
		return core.Record{}, false, false
	}
	attempts := assertion.Invocations
	durationMS, partial := normalizeJestDuration(assertion.Duration)
	record, ok := testCaseRecord(assertion.Title, assertion.AncestorTitles, status, durationMS, &attempts, disposition, suiteOrdinal, testcaseOrdinal, pathInfo, ref, input, producer)
	if !ok {
		return core.Record{}, false, false
	}
	excerptPartial := attachJestFailureExcerpt(&record, assertion.FailureMessages, input.RepositoryRoot)
	return record, partial || excerptPartial, record.Validate() == nil
}

func normalizeAssertionV29(assertion assertionV29, suiteOrdinal, testcaseOrdinal int, pathInfo jestFilePath, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) (core.Record, bool, bool) {
	status, disposition, ok := mapAssertionStatus(assertion.Status, nil)
	if !ok || assertion.Invocations < 1 || assertion.Invocations > 1<<20 || assertion.Title == "" {
		return core.Record{}, false, false
	}
	attempts := assertion.Invocations
	durationMS, partial := normalizeJestDuration(assertion.Duration)
	record, ok := testCaseRecord(assertion.Title, assertion.AncestorTitles, status, durationMS, &attempts, disposition, suiteOrdinal, testcaseOrdinal, pathInfo, ref, input, producer)
	if !ok {
		return core.Record{}, false, false
	}
	excerptPartial := attachJestFailureExcerpt(&record, assertion.FailureMessages, input.RepositoryRoot)
	return record, partial || excerptPartial, record.Validate() == nil
}

func attachJestFailureExcerpt(record *core.Record, messages []string, workspaceRoot string) bool {
	if record == nil || record.TestCase == nil || record.TestCase.Status == core.TestPassed || len(messages) == 0 {
		return false
	}
	excerpt, ok := core.NormalizeFailureExcerpt(messages[0], "jest", workspaceRoot)
	if !ok {
		return true
	}
	record.SchemaVersion = core.RecordSchemaVersionV3
	record.TestCase.FailureExcerpt = &excerpt
	return false
}

func normalizeJestDuration(value *float64) (int64, bool) {
	if value == nil {
		return 0, false
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > float64(math.MaxInt64) {
		return 0, true
	}
	return int64(*value), false
}

func mapAssertionStatus(status string, failing *bool) (core.TestStatus, *core.ProducerTestDisposition, bool) {
	switch status {
	case "passed":
		if failing != nil && *failing {
			return core.TestPassed, jestDisposition("jest:failing_expected"), true
		}
		return core.TestPassed, nil, true
	case "failed":
		if failing != nil && *failing {
			return core.TestFailed, jestDisposition("jest:failing_unexpected"), true
		}
		return core.TestFailed, nil, true
	case "pending":
		return core.TestSkipped, jestDisposition("jest:pending"), true
	case "todo":
		return core.TestSkipped, jestDisposition("jest:todo"), true
	default:
		return "", nil, false
	}
}

func normalizeSuiteRecord(name, status string, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) (core.Record, bool) {
	mapped, disposition, ok := mapSuiteStatus(status)
	if !ok || name == "" {
		return core.Record{}, false
	}
	record := core.Record{
		SchemaVersion: core.SchemaVersion, RecordKind: core.RecordTestSuite, Authority: core.AuthorityMechanical,
		DerivationMethod: core.DerivationNativeFieldMapping, Producer: producer, OperationID: input.OperationID, SourceRef: ref,
		TestSuite: &core.TestSuite{Name: name, Status: mapped, ProducerDisposition: disposition},
	}
	return record, record.Validate() == nil
}

func mapSuiteStatus(status string) (core.TestStatus, *core.ProducerTestDisposition, bool) {
	switch status {
	case "failed":
		return core.TestFailed, nil, true
	case "passed":
		return core.TestPassed, nil, true
	case "skipped":
		return core.TestSkipped, nil, true
	case "focused":
		return core.TestPassed, jestDisposition("jest:suite_focused"), true
	default:
		return "", nil, false
	}
}

func testCaseRecord(name string, ancestors []string, status core.TestStatus, durationMS int64, attempts *int, disposition *core.ProducerTestDisposition, suiteOrdinal, testcaseOrdinal int, pathInfo jestFilePath, ref core.StructuredInputRef, input app.InputContext, producer core.Producer) (core.Record, bool) {
	entry := core.ArtifactTestEntryRef{ArtifactBlobID: ref.ArtifactBlob.BlobID, SuiteOrdinal: suiteOrdinal, TestcaseOrdinal: testcaseOrdinal}
	recordID, err := core.ArtifactTestRecordID(input.DerivationKey, entry)
	if err != nil {
		return core.Record{}, false
	}
	var address *core.ProducerTestAddress
	if pathInfo.repoRelative {
		address = &core.ProducerTestAddress{Namespace: "jest", VocabularyVersion: jestVocabularyV1, SuiteName: pathInfo.suiteName, Classname: strings.Join(ancestors, " > "), Name: name}
	}
	record := core.Record{
		SchemaVersion: core.SchemaVersion, RecordKind: core.RecordTestCase, Authority: core.AuthorityMechanical,
		DerivationMethod: core.DerivationNativeFieldMapping, Producer: producer, OperationID: input.OperationID, RecordID: recordID, SourceRef: ref,
		TestCase: &core.TestCase{Name: name, Status: status, DurationMS: durationMS, AttemptCount: attempts, ProducerDisposition: disposition, ProducerAddress: address, ArtifactEntry: &entry},
	}
	return record, record.Validate() == nil
}

func jestDisposition(code string) *core.ProducerTestDisposition {
	return &core.ProducerTestDisposition{Namespace: "jest", VocabularyVersion: jestVocabularyV1, Code: code}
}

func summarizeRecords(records []core.Record) app.ParseSummary {
	summary := app.ParseSummary{Records: len(records)}
	for _, record := range records {
		if record.TestCase == nil {
			continue
		}
		switch record.TestCase.Status {
		case core.TestPassed:
			summary.TestPassed++
		case core.TestFailed:
			summary.TestFailed++
		case core.TestSkipped:
			summary.TestSkipped++
		default:
			panic(fmt.Sprintf("unexpected normalized Jest status %q", record.TestCase.Status))
		}
	}
	return summary
}
