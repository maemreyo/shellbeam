package outputview

import (
	"bytes"
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func (s *Service) readTail(ctx context.Context, out Result, sel Selector, state CursorState) (Result, error) {
	if sel.TailBytes > 0 {
		if state.Phase != "" {
			return Result{}, cursorFailure(failure.OutputContinuationInvalid, "tail_bytes")
		}
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
	return s.readTailLines(ctx, out, sel, state)
}

func (s *Service) readTailLines(ctx context.Context, out Result, sel Selector, state CursorState) (Result, error) {
	if out.FrozenCutBytes == 0 {
		return out, nil
	}
	if state.Phase == "tail_emit" {
		return s.emitTailSegment(ctx, out, sel, state.Boundary, state.Offset)
	}
	initial := state.Phase == ""
	if !initial && state.Phase != "tail_seek" {
		return Result{}, cursorFailure(failure.OutputContinuationInvalid, "tail_phase")
	}
	seekEnd := out.FrozenCutBytes
	progress := 0
	emittedBoundary := out.FrozenCutBytes
	if !initial {
		seekEnd = state.Offset
		progress = state.Progress
		emittedBoundary = state.Boundary
	}
	scanStart := max(int64(0), seekEnd-int64(MaxWorkBytes))
	data, err := s.readExactWindow(ctx, out.SessionID, scanStart, int(seekEnd-scanStart))
	if err != nil {
		return Result{}, err
	}
	tailStart, progress, found := scanTailStart(data, scanStart, out.FrozenCutBytes, sel.TailLines, progress)
	if !found && scanStart == 0 {
		tailStart, found = 0, true
	}
	if found {
		end := emittedBoundary
		if initial {
			end = out.FrozenCutBytes
		}
		return s.emitTailSegment(ctx, out, sel, tailStart, end)
	}
	if initial {
		returnedStart := max(scanStart, out.FrozenCutBytes-int64(MaxReturnBytes))
		segment, readErr := s.readExactWindow(ctx, out.SessionID, returnedStart, int(out.FrozenCutBytes-returnedStart))
		if readErr != nil {
			return Result{}, readErr
		}
		out.Text = safeText(segment)
		out.Ranges = []RawRange{{Start: returnedStart, End: out.FrozenCutBytes}}
		out.Truncated = true
		emittedBoundary = returnedStart
	}
	next := CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: scanStart, Progress: progress, Phase: "tail_seek", Boundary: emittedBoundary}
	return s.withContinuation(out, sel, next)
}

func (s *Service) emitTailSegment(ctx context.Context, out Result, sel Selector, targetStart, end int64) (Result, error) {
	if targetStart < 0 || end < targetStart || end > out.FrozenCutBytes {
		return Result{}, cursorFailure(failure.OutputContinuationInvalid, "tail_range")
	}
	start := max(targetStart, end-int64(MaxReturnBytes))
	data, err := s.readExactWindow(ctx, out.SessionID, start, int(end-start))
	if err != nil {
		return Result{}, err
	}
	out.Text = safeText(data)
	out.Ranges = []RawRange{{Start: start, End: end}}
	out.Truncated = targetStart > 0 || start > targetStart
	if start > targetStart {
		next := CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: start, Phase: "tail_emit", Boundary: targetStart}
		return s.withContinuation(out, sel, next)
	}
	return out, nil
}

func scanTailStart(data []byte, absoluteStart, frozenCut int64, want, progress int) (int64, int, bool) {
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] != '\n' {
			continue
		}
		pos := absoluteStart + int64(i)
		if pos == frozenCut-1 {
			continue
		}
		progress++
		if progress == want {
			return pos + 1, progress, true
		}
	}
	return 0, progress, false
}

func (s *Service) readLines(ctx context.Context, out Result, sel Selector, state CursorState) (Result, error) {
	if out.FrozenCutBytes == 0 {
		if sel.StartLine == 1 {
			out.Ranges = []RawRange{{Start: 0, End: 0}}
			return out, nil
		}
		return Result{}, outOfRange(out.SessionID, "line")
	}
	if state.Phase == "" {
		state = CursorState{FrozenCutBytes: out.FrozenCutBytes, Offset: 0, Line: 1, Phase: "lines"}
	}
	if state.Phase != "lines" || state.Line < 1 || state.Progress > sel.MaxLines {
		return Result{}, cursorFailure(failure.OutputContinuationInvalid, "lines_phase")
	}
	for state.Progress == 0 && state.Line < sel.StartLine {
		if state.Offset >= out.FrozenCutBytes {
			return Result{}, outOfRange(out.SessionID, "line")
		}
		work := int(min(int64(MaxWorkBytes), out.FrozenCutBytes-state.Offset))
		data, err := s.readExactWindow(ctx, out.SessionID, state.Offset, work)
		if err != nil {
			return Result{}, err
		}
		consumed := 0
		for state.Line < sel.StartLine {
			i := bytes.IndexByte(data[consumed:], '\n')
			if i < 0 {
				break
			}
			consumed += i + 1
			state.Line++
			state.WithinLine = false
		}
		state.Offset += int64(consumed)
		if state.Line >= sel.StartLine {
			break
		}
		if consumed < len(data) {
			state.Offset += int64(len(data) - consumed)
			state.WithinLine = true
		}
		if state.Offset >= out.FrozenCutBytes {
			return Result{}, outOfRange(out.SessionID, "line")
		}
		return s.withContinuation(out, sel, state)
	}
	if state.Progress >= sel.MaxLines {
		return out, nil
	}
	return s.emitLines(ctx, out, sel, state)
}

func (s *Service) emitLines(ctx context.Context, out Result, sel Selector, state CursorState) (Result, error) {
	remainingBytes := out.FrozenCutBytes - state.Offset
	if remainingBytes <= 0 {
		return out, nil
	}
	readMax := int(min(int64(MaxReturnBytes+4), min(int64(MaxWorkBytes), remainingBytes)))
	data, err := s.readExactWindow(ctx, out.SessionID, state.Offset, readMax)
	if err != nil {
		return Result{}, err
	}
	needLines := sel.MaxLines - state.Progress
	candidateEnd, completeLines := boundedLineEnd(data, needLines)
	candidate := data[:candidateEnd]
	text, consumed, clipped := receipt.VisibleOutput(candidate, MaxReturnBytes)
	if consumed == 0 && len(candidate) > 0 {
		return Result{}, failure.New(failure.OutputUnavailable, map[string]string{"session_id": out.SessionID, "reason": "utf8_progress"}, nil)
	}
	start := state.Offset
	end := start + int64(consumed)
	out.Text = text
	out.Ranges = []RawRange{{Start: start, End: end}}
	consumedData := candidate[:consumed]
	newlines := bytes.Count(consumedData, []byte{'\n'})
	state.Progress += newlines
	state.Line += newlines
	state.Offset = end
	state.WithinLine = consumed > 0 && consumedData[consumed-1] != '\n'
	if state.Offset == out.FrozenCutBytes && state.WithinLine {
		state.Progress++
		state.WithinLine = false
	}
	if state.Progress >= sel.MaxLines {
		out.Truncated = state.Offset < out.FrozenCutBytes
		return out, nil
	}
	if completeLines >= needLines && !clipped {
		out.Truncated = state.Offset < out.FrozenCutBytes
		return out, nil
	}
	if state.Offset < out.FrozenCutBytes {
		out.Truncated = true
		return s.withContinuation(out, sel, state)
	}
	return out, nil
}

func boundedLineEnd(data []byte, need int) (int, int) {
	if need <= 0 {
		return 0, 0
	}
	pos, lines := 0, 0
	for pos < len(data) && lines < need {
		i := bytes.IndexByte(data[pos:], '\n')
		if i < 0 {
			return len(data), lines
		}
		pos += i + 1
		lines++
	}
	return pos, lines
}
