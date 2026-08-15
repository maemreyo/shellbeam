package outputview

import (
	"bytes"
	"context"
	"fmt"
	"regexp"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const maxExcerptBytes = 512

type searchMatcher struct {
	literal []byte
	regex   *regexp.Regexp
}

func newSearchMatcher(sel Selector) (searchMatcher, error) {
	if sel.SearchMode == SearchLiteral && sel.CaseSensitive {
		return searchMatcher{literal: []byte(sel.Pattern)}, nil
	}
	pattern := sel.Pattern
	if sel.SearchMode == SearchLiteral {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !sel.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	rx, err := regexp.Compile(pattern)
	if err != nil {
		return searchMatcher{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_regex"}, err)
	}
	return searchMatcher{regex: rx}, nil
}

func (m searchMatcher) all(data []byte) [][]int {
	if m.regex != nil {
		return m.regex.FindAllIndex(data, -1)
	}
	var out [][]int
	for at := 0; at <= len(data)-len(m.literal); {
		i := bytes.Index(data[at:], m.literal)
		if i < 0 {
			break
		}
		start := at + i
		out = append(out, []int{start, start + len(m.literal)})
		at = start + max(1, len(m.literal))
	}
	return out
}

func (s *Service) readSearch(ctx context.Context, out Result, sel Selector, state CursorState) (Result, error) {
	matcher, err := newSearchMatcher(sel)
	if err != nil {
		return Result{}, err
	}
	if state.Phase == "" {
		state = CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: 0, Line: 1, Phase: "search"}
	}
	if state.Line < 1 {
		state.Line = 1
	}
	remaining := out.FrozenCutBytes - state.Offset
	if remaining <= 0 {
		return out, nil
	}
	work := int(min(int64(MaxWorkBytes), remaining))
	data, err := s.readExactWindow(ctx, out.SessionID, state.Offset, work)
	if err != nil {
		return Result{}, err
	}
	pos, lineNo := 0, state.Line
	for pos < len(data) {
		lineStart := pos
		nl := bytes.IndexByte(data[pos:], '\n')
		complete := nl >= 0
		lineEnd := len(data)
		next := len(data)
		if complete {
			lineEnd, next = pos+nl, pos+nl+1
		}
		line := data[lineStart:lineEnd]
		indexes := matcher.all(line)
		skip := 0
		if int64(lineStart)+state.Offset == state.Offset && state.Progress > 0 {
			skip = min(state.Progress, len(indexes))
		}
		for i := skip; i < len(indexes); i++ {
			out.Matches = append(out.Matches, makeSearchMatch(line, indexes[i], state.Offset+int64(lineStart), lineNo))
			if len(out.Matches) == sel.MaxMatches {
				var nextState CursorState
				if i+1 < len(indexes) {
					nextState = CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: state.Offset + int64(lineStart), Line: lineNo, Progress: i + 1, WithinLine: state.WithinLine, Phase: "search"}
				} else {
					nextOffset := state.Offset + int64(next)
					nextLine := lineNo
					within := !complete && nextOffset < out.FrozenCutBytes
					if complete {
						nextLine++
					}
					if nextOffset >= out.FrozenCutBytes {
						return out, nil
					}
					nextState = CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: nextOffset, Line: nextLine, WithinLine: within, Phase: "search"}
				}
				return s.withContinuation(out, sel, nextState)
			}
		}
		state.Progress = 0
		pos = next
		if complete {
			lineNo++
		}
		if !complete {
			break
		}
	}
	nextOffset := state.Offset + int64(len(data))
	if nextOffset < out.FrozenCutBytes {
		nextState := CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: nextOffset, Line: lineNo, WithinLine: true, Phase: "search"}
		out.Partial = true
		return s.withContinuation(out, sel, nextState)
	}
	return out, nil
}

func makeSearchMatch(line []byte, index []int, lineStart int64, lineNo int) Match {
	excerptRaw := line
	truncated := false
	if len(excerptRaw) > maxExcerptBytes {
		excerptRaw = excerptRaw[:maxExcerptBytes]
		truncated = true
	}
	return Match{
		Line:      lineNo,
		RawRange:  RawRange{Start: lineStart + int64(index[0]), End: lineStart + int64(index[1])},
		Excerpt:   renderedText(excerptRaw),
		Truncated: truncated,
	}
}

func (s *Service) withContinuation(out Result, sel Selector, state CursorState) (Result, error) {
	if s.cursor == nil {
		return Result{}, failure.New(failure.OutputUnavailable, map[string]string{"session_id": out.SessionID, "reason": "continuation"}, fmt.Errorf("cursor codec unavailable"))
	}
	token, err := s.cursor.Encode(out.SessionID, sel, state)
	if err != nil {
		return Result{}, err
	}
	out.Continuation = token
	out.Partial = true
	return out, nil
}
