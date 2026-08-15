package outputview

import (
	"bytes"
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) Read(ctx context.Context, req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, failure.New(failure.InvalidInput, map[string]string{"field": "selector"}, err)
	}
	extent, err := s.store.OutputExtent(ctx, operation.SessionID(req.SessionID))
	if err != nil {
		return Result{}, failure.New(failure.OutputUnavailable, map[string]string{"session_id": req.SessionID, "reason": "extent"}, err)
	}
	if err := retentionError(req.SessionID, extent.State); err != nil {
		return Result{}, err
	}
	base := Result{SchemaVersion: 1, SessionID: req.SessionID, SelectorKind: req.Selector.Kind, RetentionState: extent.State, FrozenCutBytes: extent.Bytes}
	switch req.Selector.Kind {
	case SelectorRawRange:
		return s.readRaw(ctx, base, req.Selector)
	case SelectorTail:
		return s.readTail(ctx, base, req.Selector)
	case SelectorLines:
		return s.readLines(ctx, base, req.Selector)
	case SelectorPreview:
		return s.readPreview(ctx, base, req.Selector)
	default:
		return Result{}, failure.New(failure.InvalidInput, map[string]string{"field": "selector"}, fmt.Errorf("selector not implemented"))
	}
}

func retentionError(sessionID string, state RetentionState) error {
	switch state {
	case RetentionRetained:
		return nil
	case RetentionCompacted:
		return failure.New(failure.OutputCompacted, map[string]string{"session_id": sessionID}, nil)
	default:
		return failure.New(failure.OutputUnavailable, map[string]string{"session_id": sessionID, "reason": "retention"}, nil)
	}
}

func (s *Service) readRaw(ctx context.Context, out Result, sel Selector) (Result, error) {
	if sel.StartByte > out.FrozenCutBytes {
		return Result{}, outOfRange(out.SessionID, "byte")
	}
	want := min(int64(sel.MaxBytes+4), out.FrozenCutBytes-sel.StartByte)
	data, err := s.readExactWindow(ctx, out.SessionID, sel.StartByte, int(want))
	if err != nil {
		return Result{}, err
	}
	text, consumed, clipped := receipt.VisibleOutput(data, sel.MaxBytes)
	end := sel.StartByte + int64(consumed)
	out.Text = text
	out.Ranges = []RawRange{{Start: sel.StartByte, End: end}}
	out.Truncated = clipped || end < out.FrozenCutBytes
	return out, nil
}

func (s *Service) readTail(ctx context.Context, out Result, sel Selector) (Result, error) {
	if sel.TailBytes > 0 {
		start := max(int64(0), out.FrozenCutBytes-int64(sel.TailBytes))
		data, err := s.readExactWindow(ctx, out.SessionID, start, int(out.FrozenCutBytes-start))
		if err != nil {
			return Result{}, err
		}
		out.Text = safeText(data)
		out.Ranges = []RawRange{{Start: start, End: out.FrozenCutBytes}}
		out.Truncated = start > 0
		return out, nil
	}
	return s.readTailLines(ctx, out, sel.TailLines)
}

func (s *Service) readTailLines(ctx context.Context, out Result, lines int) (Result, error) {
	work := min(out.FrozenCutBytes, int64(MaxWorkBytes))
	start := out.FrozenCutBytes - work
	data, err := s.readExactWindow(ctx, out.SessionID, start, int(work))
	if err != nil {
		return Result{}, err
	}
	rel, foundAll := tailLineStart(data, lines)
	if !foundAll && start > 0 {
		out.Partial = true
	}
	selected := data[rel:]
	absolute := start + int64(rel)
	if len(selected) > MaxReturnBytes {
		drop := len(selected) - MaxReturnBytes
		selected = selected[drop:]
		absolute += int64(drop)
		out.Partial = true
	}
	out.Text = safeText(selected)
	out.Ranges = []RawRange{{Start: absolute, End: out.FrozenCutBytes}}
	out.Truncated = absolute > 0 || out.Partial
	return out, nil
}

func (s *Service) readLines(ctx context.Context, out Result, sel Selector) (Result, error) {
	work := min(out.FrozenCutBytes, int64(MaxWorkBytes))
	data, err := s.readExactWindow(ctx, out.SessionID, 0, int(work))
	if err != nil {
		return Result{}, err
	}
	start, end, found := selectLines(data, sel.StartLine, sel.MaxLines)
	if !found {
		if work < out.FrozenCutBytes {
			out.Partial = true
			return out, nil
		}
		return Result{}, outOfRange(out.SessionID, "line")
	}
	selected := data[start:end]
	text, consumed, clipped := receipt.VisibleOutput(selected, MaxReturnBytes)
	selectedEnd := start + consumed
	out.Text = text
	out.Ranges = []RawRange{{Start: int64(start), End: int64(selectedEnd)}}
	out.Partial = clipped || (int64(end) == work && work < out.FrozenCutBytes && (end == 0 || data[end-1] != '\n'))
	out.Truncated = int64(selectedEnd) < out.FrozenCutBytes
	return out, nil
}

func (s *Service) readExactWindow(ctx context.Context, sessionID string, start int64, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, nil
	}
	data, next, err := s.store.ReadOutput(ctx, operation.SessionID(sessionID), start, maxBytes)
	if err != nil {
		return nil, failure.New(failure.OutputUnavailable, map[string]string{"session_id": sessionID, "reason": "read"}, err)
	}
	if next != start+int64(len(data)) {
		return nil, failure.New(failure.OutputUnavailable, map[string]string{"session_id": sessionID, "reason": "accounting"}, nil)
	}
	return data, nil
}

func selectLines(data []byte, startLine, maxLines int) (int, int, bool) {
	line, start := 1, 0
	for line < startLine {
		i := bytes.IndexByte(data[start:], '\n')
		if i < 0 {
			return 0, 0, false
		}
		start += i + 1
		line++
	}
	if start >= len(data) && startLine > 1 {
		return 0, 0, false
	}
	end, count := start, 0
	for end < len(data) && count < maxLines {
		i := bytes.IndexByte(data[end:], '\n')
		if i < 0 {
			end = len(data)
			count++
			break
		}
		end += i + 1
		count++
	}
	return start, end, count > 0 || (startLine == 1 && len(data) == 0)
}

func tailLineStart(data []byte, want int) (int, bool) {
	if len(data) == 0 {
		return 0, true
	}
	pos := len(data)
	if data[pos-1] == '\n' {
		pos--
	}
	for range want {
		i := bytes.LastIndexByte(data[:pos], '\n')
		if i < 0 {
			return 0, false
		}
		pos = i
	}
	return pos + 1, true
}

func safeText(data []byte) string {
	text, _, _ := receipt.VisibleOutput(data, len(data))
	return text
}

func outOfRange(sessionID, reason string) error {
	return failure.New(failure.OutputOutOfRange, map[string]string{"session_id": sessionID, "reason": reason}, nil)
}
