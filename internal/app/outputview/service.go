package outputview

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type Service struct {
	store  Store
	cursor *CursorCodec
}

func New(store Store) *Service { return &Service{store: store} }
func NewWithCursor(store Store, cursor *CursorCodec) *Service {
	return &Service{store: store, cursor: cursor}
}

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
	var cursorState CursorState
	frozenCut := extent.Bytes
	if req.Continuation != "" {
		if s.cursor == nil {
			return Result{}, failure.New(failure.OutputContinuationInvalid, map[string]string{"reason": "codec_unavailable"}, nil)
		}
		cursorState, err = s.cursor.Decode(req.Continuation, req.SessionID, req.Selector)
		if err != nil {
			return Result{}, err
		}
		if cursorState.FrozenCutBytes > extent.Bytes {
			return Result{}, failure.New(failure.OutputUnavailable, map[string]string{"session_id": req.SessionID, "reason": "cut_unavailable"}, nil)
		}
		frozenCut = cursorState.FrozenCutBytes
	}
	base := Result{SchemaVersion: 1, SessionID: req.SessionID, SelectorKind: req.Selector.Kind, RetentionState: extent.State, FrozenCutBytes: frozenCut}
	switch req.Selector.Kind {
	case SelectorRawRange:
		return s.readRaw(ctx, base, req.Selector)
	case SelectorTail:
		return s.readTail(ctx, base, req.Selector, cursorState)
	case SelectorLines:
		return s.readLines(ctx, base, req.Selector, cursorState)
	case SelectorPreview:
		return s.readPreview(ctx, base, req.Selector)
	case SelectorSearch:
		return s.readSearch(ctx, base, req.Selector, cursorState)
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

func safeText(data []byte) string {
	text, _, _ := receipt.VisibleOutput(data, len(data))
	return text
}

func outOfRange(sessionID, reason string) error {
	return failure.New(failure.OutputOutOfRange, map[string]string{"session_id": sessionID, "reason": reason}, nil)
}
