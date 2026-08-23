package pytestjunit

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func (Adapter) Parse(ctx context.Context, ref core.StructuredInputRef, reader app.Reader, limits app.Limits) (app.ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return app.ParseResult{}, err
	}
	if reader == nil || limits.Validate() != nil || ref.Validate() != nil || ref.Kind != core.StructuredInputArtifactBlob || ref.ArtifactBlob == nil {
		return app.ParseResult{}, fmt.Errorf("pytest junit adapter requires artifact input")
	}
	input, err := reader.DescribeInput(ctx, ref)
	if err != nil {
		return app.ParseResult{}, err
	}
	if err := input.Validate(); err != nil || input.DerivationKey == "" || input.OperationID != ref.ArtifactBlob.OperationID {
		return app.ParseResult{}, fmt.Errorf("pytest junit derivation context unavailable")
	}
	deadline := time.Now().Add(limits.MaxDuration)
	stream := &artifactStream{ctx: ctx, reader: reader, ref: ref, size: ref.ArtifactBlob.Size, maxBytes: limits.MaxBytes, deadline: deadline}
	parser := junitParser{
		ctx: ctx, decoder: xml.NewDecoder(stream), ref: ref, input: input,
		producer:   core.Producer{AdapterID: (Adapter{}).ID(), AdapterVersion: adapterVersion, CapabilityVersion: capabilityVersion},
		maxRecords: min(limits.MaxRecords, maxPytestJUnitRecords), fieldLimit: min(limits.MaxStringBytes, maxXMLFieldBytes), deadline: deadline,
		coverage: semanticsCoverage(),
	}
	return parser.parse(), nil
}

type artifactStream struct {
	ctx      context.Context
	reader   app.Reader
	ref      core.StructuredInputRef
	size     int64
	offset   int64
	read     int64
	maxBytes int64
	deadline time.Time
}

func (r *artifactStream) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if time.Now().After(r.deadline) {
		return 0, errBudget
	}
	if r.offset >= r.size {
		return 0, io.EOF
	}
	budget := r.maxBytes - r.read
	if budget <= 0 {
		return 0, errBudget
	}
	want := minInt64(int64(len(p)), r.size-r.offset, budget, 64<<10)
	if want <= 0 {
		return 0, errBudget
	}
	data, err := r.reader.ReadInputRange(r.ctx, r.ref, r.offset, int(want))
	if err != nil {
		return 0, err
	}
	if len(data) == 0 || int64(len(data)) > want {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, data)
	r.offset += int64(n)
	r.read += int64(n)
	return n, nil
}

type junitParser struct {
	ctx        context.Context
	decoder    *xml.Decoder
	ref        core.StructuredInputRef
	input      app.InputContext
	producer   core.Producer
	maxRecords int
	fieldLimit int
	deadline   time.Time
	coverage   *core.ProducerSemanticsCoverage

	depth        int
	elements     int
	rootSeen     bool
	rootName     string
	rootClosed   bool
	suiteOrdinal int
	currentSuite *suiteState
	currentCase  *testcaseState
	textBytes    []int
	records      []core.Record
	summary      app.ParseSummary
	partial      bool
}

func (p *junitParser) parse() app.ParseResult {
	for {
		if err := p.ctx.Err(); err != nil {
			return app.ParseResult{}
		}
		if time.Now().After(p.deadline) {
			return p.failure(errBudget)
		}
		token, err := p.decoder.Token()
		if errors.Is(err, io.EOF) {
			if !p.rootSeen || !p.rootClosed || p.depth != 0 || p.currentSuite != nil || p.currentCase != nil {
				return p.failure(errMalformed)
			}
			if p.partial {
				return p.result(core.ParsePartial, core.CompletenessPartial)
			}
			return p.result(core.ParseComplete, core.CompletenessComplete)
		}
		if err != nil {
			if errors.Is(err, errBudget) {
				return p.failure(errBudget)
			}
			return p.failure(errMalformed)
		}
		switch t := token.(type) {
		case xml.StartElement:
			if err := p.start(t); err != nil {
				return p.failure(err)
			}
		case xml.EndElement:
			if err := p.end(t); err != nil {
				return p.failure(err)
			}
		case xml.CharData:
			if p.depth > 0 {
				idx := p.depth - 1
				p.textBytes[idx] += len(t)
				if p.textBytes[idx] > p.fieldLimit {
					return p.failure(errBudget)
				}
			}
		case xml.Directive:
			return p.failure(errMalformed)
		case xml.ProcInst, xml.Comment:
			// Non-semantic and bounded by the input byte ceiling.
		}
	}
}

func (p *junitParser) start(el xml.StartElement) error {
	p.depth++
	if p.depth > maxXMLDepth {
		return errBudget
	}
	p.elements++
	if p.elements > maxXMLElements || len(el.Attr) > maxXMLAttributes {
		return errBudget
	}
	for _, attr := range el.Attr {
		if len(attr.Value) > p.fieldLimit || !utf8.ValidString(attr.Value) || strings.ContainsRune(attr.Value, 0) {
			return errBudget
		}
	}
	p.textBytes = append(p.textBytes, 0)
	name := el.Name.Local
	parent := p.parentName()
	if !p.rootSeen {
		if p.depth != 1 || el.Name.Space != "" || (name != "testsuites" && name != "testsuite") {
			return errMalformed
		}
		p.rootSeen, p.rootName = true, name
	}
	if p.rootClosed {
		return errMalformed
	}
	if p.rootName == "testsuites" && p.depth == 2 && name != "testsuite" {
		return errMalformed
	}
	if isSemanticElement(name) && el.Name.Space != "" {
		return errMalformed
	}

	switch name {
	case "testsuites":
		if p.depth != 1 {
			return errMalformed
		}
	case "testsuite":
		if p.currentSuite != nil || !(p.depth == 1 && p.rootName == "testsuite" || p.depth == 2 && parent == "testsuites") {
			return errMalformed
		}
		suite, err := parseSuite(el, p.suiteOrdinal, p.depth, p.fieldLimit)
		if err != nil {
			return err
		}
		p.currentSuite = &suite
		p.suiteOrdinal++
	case "testcase":
		if p.currentSuite == nil || p.currentCase != nil || parent != "testsuite" {
			return errMalformed
		}
		item, err := parseTestcase(el, p.depth, p.fieldLimit)
		if err != nil {
			return err
		}
		p.currentCase = &item
	case "failure", "error", "skipped":
		if p.currentCase == nil || parent != "testcase" {
			return p.handleNonSemanticStart(name, parent)
		}
		p.applyOutcome(name, el)
	case "properties", "property", "system-out", "system-err":
		// Known non-semantic JUnit/pytest output surfaces. They never affect status.
	default:
		if p.currentCase != nil && p.depth == p.currentCase.tagDepth+1 {
			p.currentCase.partial = true
			p.partial = true
		} else if p.currentSuite != nil && p.depth == p.currentSuite.tagDepth+1 {
			p.partial = true
		} else if p.depth <= 2 {
			return errMalformed
		}
	}
	return nil
}

func (p *junitParser) handleNonSemanticStart(name, parent string) error {
	if p.currentSuite != nil && (parent == "testsuite" || p.currentCase != nil) {
		p.partial = true
		if p.currentCase != nil {
			p.currentCase.partial = true
		}
		return nil
	}
	return errMalformed
}

func (p *junitParser) applyOutcome(name string, el xml.StartElement) {
	item := p.currentCase
	if item.outcomeSeen {
		item.invalid = true
		p.partial = true
		return
	}
	item.outcomeSeen = true
	switch name {
	case "failure":
		item.status = core.TestFailed
	case "error":
		item.status = core.TestError
	case "skipped":
		item.status = core.TestSkipped
		typeValue, _ := attrValue(el, "type")
		switch typeValue {
		case "pytest.skip":
			item.disposition = &core.ProducerTestDisposition{Namespace: "pytest", VocabularyVersion: pytestVocabularyV1, Code: "pytest:skip"}
		case "pytest.xfail":
			item.disposition = &core.ProducerTestDisposition{Namespace: "pytest", VocabularyVersion: pytestVocabularyV1, Code: "pytest:xfail"}
		default:
			item.partial = true
			item.diagnostic = true
			p.partial = true
		}
	}
}

func (p *junitParser) end(el xml.EndElement) error {
	if p.depth <= 0 || len(p.textBytes) != p.depth {
		return errMalformed
	}
	name := el.Name.Local
	if p.currentCase != nil && name == "testcase" && p.depth == p.currentCase.tagDepth {
		if err := p.emitTestcase(); err != nil {
			return err
		}
		p.currentCase = nil
	}
	if p.currentSuite != nil && name == "testsuite" && p.depth == p.currentSuite.tagDepth {
		if p.currentCase != nil {
			return errMalformed
		}
		if err := p.emitSuite(); err != nil {
			return err
		}
		p.currentSuite = nil
	}
	if p.depth == 1 && name == p.rootName {
		p.rootClosed = true
	}
	p.textBytes = p.textBytes[:len(p.textBytes)-1]
	p.depth--
	return nil
}

func (p *junitParser) emitTestcase() error {
	item, suite := p.currentCase, p.currentSuite
	if item == nil || suite == nil {
		return errMalformed
	}
	ordinal := suite.caseOrdinal
	suite.caseOrdinal++
	if item.invalid {
		return nil
	}
	entry := core.ArtifactTestEntryRef{ArtifactBlobID: p.ref.ArtifactBlob.BlobID, SuiteOrdinal: suite.ordinal, TestcaseOrdinal: ordinal}
	recordID, err := core.ArtifactTestRecordID(p.input.DerivationKey, entry)
	if err != nil {
		return errMalformed
	}
	address := &core.ProducerTestAddress{Namespace: "pytest", VocabularyVersion: pytestVocabularyV1, SuiteName: suite.name, Classname: item.classname, Name: item.name}
	testcase := &core.TestCase{Name: item.name, Status: item.status, DurationMS: item.durationMS, ProducerDisposition: item.disposition, ProducerAddress: address, ArtifactEntry: &entry}
	record := core.Record{SchemaVersion: core.SchemaVersion, RecordKind: core.RecordTestCase, Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping, Producer: p.producer, OperationID: p.input.OperationID, RecordID: recordID, SourceRef: p.ref, TestCase: testcase}
	if err := record.Validate(); err != nil {
		return errMalformed
	}
	if err := p.appendRecord(record); err != nil {
		return err
	}
	if item.diagnostic {
		diagnostic := core.Record{
			SchemaVersion: core.SchemaVersion, RecordKind: core.RecordDiagnostic, Authority: core.AuthorityMechanical,
			DerivationMethod: core.DerivationNativeFieldMapping, Producer: p.producer, OperationID: p.input.OperationID, SourceRef: p.ref,
			Diagnostic: &core.Diagnostic{Severity: core.SeverityWarning, Code: "pytest_skipped_subtype_unavailable", Message: "pytest skipped subtype is not mechanically recognized", Location: source.SourceLocation{Kind: source.LocationProviderReported, ProviderReported: &source.ProviderReportedLocation{Origin: source.OriginExternal, NormalizationQuality: source.NormalizationUnavailable}}},
		}
		if err := diagnostic.Validate(); err != nil {
			return errMalformed
		}
		if err := p.appendRecord(diagnostic); err != nil {
			return err
		}
		p.summary.Warnings++
	}
	switch item.status {
	case core.TestPassed:
		p.summary.TestPassed++
	case core.TestSkipped:
		p.summary.TestSkipped++
	case core.TestFailed, core.TestError:
		p.summary.TestFailed++
	}
	return nil
}

func (p *junitParser) emitSuite() error {
	suite := p.currentSuite
	aggregate := &core.TestSuiteAggregate{Tests: suite.tests, Failures: suite.failures, Errors: suite.errors, Skipped: suite.skipped}
	status := core.TestPassed
	switch {
	case suite.errors > 0:
		status = core.TestError
	case suite.failures > 0:
		status = core.TestFailed
	case suite.tests > 0 && suite.skipped == suite.tests:
		status = core.TestSkipped
	}
	record := core.Record{SchemaVersion: core.SchemaVersion, RecordKind: core.RecordTestSuite, Authority: core.AuthorityMechanical, DerivationMethod: core.DerivationNativeFieldMapping, Producer: p.producer, OperationID: p.input.OperationID, SourceRef: p.ref, TestSuite: &core.TestSuite{Name: suite.name, Status: status, DurationMS: suite.durationMS, Aggregate: aggregate}}
	if err := record.Validate(); err != nil {
		return errMalformed
	}
	return p.appendRecord(record)
}

func (p *junitParser) appendRecord(record core.Record) error {
	if len(p.records) >= p.maxRecords {
		return errBudget
	}
	p.records = append(p.records, record)
	p.summary.Records = len(p.records)
	return nil
}

func (p *junitParser) parentName() string {
	// Structural validation is expressed using active semantic objects rather
	// than retaining arbitrary XML names. Infer only the closed parent shapes.
	if p.depth == 1 {
		return ""
	}
	if p.currentCase != nil {
		if p.depth == p.currentCase.tagDepth+1 {
			return "testcase"
		}
	}
	if p.currentSuite != nil {
		if p.depth == p.currentSuite.tagDepth+1 {
			return "testsuite"
		}
		if p.depth > p.currentSuite.tagDepth+1 && p.currentCase != nil {
			return "testcase-child"
		}
	}
	if p.rootName == "testsuites" && p.depth == 2 {
		return "testsuites"
	}
	return "other"
}

func (p *junitParser) failure(err error) app.ParseResult {
	if errors.Is(err, errBudget) {
		return p.result(core.ParseBudgetExceeded, partiality(len(p.records)))
	}
	return p.result(core.ParseMalformed, partiality(len(p.records)))
}
func partiality(records int) core.Completeness {
	if records == 0 {
		return core.CompletenessUnavailable
	}
	return core.CompletenessPartial
}
func (p *junitParser) result(outcome core.ParseOutcome, completeness core.Completeness) app.ParseResult {
	coverage := *p.coverage
	coverage.MechanicallyObservable = append([]string(nil), p.coverage.MechanicallyObservable...)
	coverage.Unavailable = append([]string(nil), p.coverage.Unavailable...)
	return app.ParseResult{Records: append([]core.Record(nil), p.records...), Outcome: outcome, Completeness: completeness, Summary: p.summary, SemanticsCoverage: &coverage}
}
