// Package gojson parses bounded native Go JSON output into structured execution facts.
package gojson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

var (
	errParseBudget    = errors.New("structured parse budget exceeded")
	errParseMalformed = errors.New("structured input malformed")
)

const adapterCapabilityVersion = 1

type boundedRangeReader struct {
	ctx      context.Context
	reader   app.Reader
	ref      core.RawOutputRef
	limits   app.Limits
	offset   int64
	read     int64
	deadline time.Time
}

func newBoundedRangeReader(ctx context.Context, reader app.Reader, ref core.RawOutputRef, limits app.Limits) *boundedRangeReader {
	return &boundedRangeReader{ctx: ctx, reader: reader, ref: ref, limits: limits, deadline: time.Now().Add(limits.MaxDuration)}
}

func (r *boundedRangeReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if time.Now().After(r.deadline) {
		return 0, errParseBudget
	}
	length := r.ref.EndByte - r.ref.StartByte
	if r.offset >= length {
		return 0, io.EOF
	}
	budget := r.limits.MaxBytes - r.read
	if budget <= 0 {
		return 0, errParseBudget
	}
	want := minInt64(int64(len(p)), length-r.offset, budget, 64<<10)
	if want <= 0 {
		return 0, errParseBudget
	}
	data, err := r.reader.ReadOutputRange(r.ctx, r.ref, r.offset, int(want))
	if err != nil {
		return 0, err
	}
	if time.Now().After(r.deadline) {
		return 0, errParseBudget
	}
	if len(data) == 0 || int64(len(data)) > want {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, data)
	r.offset += int64(n)
	r.read += int64(n)
	return n, nil
}

func parserContext(ctx context.Context, reader app.Reader, ref core.RawOutputRef, limits app.Limits) (app.InputContext, error) {
	if err := ref.Validate(); err != nil {
		return app.InputContext{}, err
	}
	if reader == nil || limits.Validate() != nil {
		return app.InputContext{}, fmt.Errorf("invalid structured parser contract")
	}
	input, err := reader.DescribeInput(ctx, ref)
	if err != nil {
		return app.InputContext{}, err
	}
	if err := input.Validate(); err != nil {
		return app.InputContext{}, err
	}
	return input, nil
}

func failureResult(records []core.Record, summary app.ParseSummary, err error) app.ParseResult {
	summary.Records = len(records)
	outcome := core.ParseUnavailable
	switch {
	case errors.Is(err, errParseBudget):
		outcome = core.ParseBudgetExceeded
	case errors.Is(err, io.ErrUnexpectedEOF), strings.Contains(err.Error(), "unexpected EOF"):
		outcome = core.ParsePartial
	case errors.Is(err, errParseMalformed):
		outcome = core.ParseMalformed
	default:
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			outcome = core.ParseMalformed
		}
	}
	completeness := core.CompletenessUnavailable
	if len(records) > 0 {
		completeness = core.CompletenessPartial
	}
	return app.ParseResult{Records: records, Outcome: outcome, Completeness: completeness, Summary: summary}
}

func completeResult(records []core.Record, summary app.ParseSummary) app.ParseResult {
	summary.Records = len(records)
	return app.ParseResult{Records: records, Outcome: core.ParseComplete, Completeness: core.CompletenessComplete, Summary: summary}
}

func appendRecord(records []core.Record, record core.Record, limits app.Limits) ([]core.Record, error) {
	if len(records) >= limits.MaxRecords {
		return records, errParseBudget
	}
	if err := record.Validate(); err != nil {
		return records, fmt.Errorf("%w: %v", errParseMalformed, err)
	}
	return append(records, record), nil
}

func checkSemanticString(value string, max int, allowEmpty bool) error {
	if len(value) > max {
		return errParseBudget
	}
	if !allowEmpty && strings.TrimSpace(value) == "" {
		return errParseMalformed
	}
	if strings.TrimSpace(value) != value && value != "" {
		return errParseMalformed
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errParseMalformed
		}
	}
	return nil
}

func checkRawDepth(raw []byte, maxDepth int) error {
	depth, inString, escaped := 0, false, false
	for _, b := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxDepth {
				return errParseBudget
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return errParseMalformed
			}
		}
	}
	if depth != 0 || inString {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func durationMS(seconds float64) (int64, error) {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/1000 {
		return 0, errParseMalformed
	}
	return int64(math.Round(seconds * 1000)), nil
}

func authorityForMethods(methods ...core.DerivationMethod) core.Authority {
	for _, method := range methods {
		if method == core.DerivationHeuristicExtraction {
			return core.AuthorityAdvisory
		}
	}
	return core.AuthorityMechanical
}

func minInt64(values ...int64) int64 {
	out := values[0]
	for _, value := range values[1:] {
		if value < out {
			out = value
		}
	}
	return out
}
