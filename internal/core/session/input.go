package session

import (
	"crypto/sha256"
	"fmt"
)

type InputRecord struct {
	Kind   string
	Offset int64
	Length int
	Hash   [32]byte
}
type InputResult struct {
	Record        InputRecord
	AcceptedBytes int
	NextOffset    int64
	Duplicate     bool
}
type InputLedger struct {
	maxQueued int
	queued    int
	tty       bool
	closed    bool
	next      int64
	entries   map[int64]InputRecord
}

func NewInputLedger(maxQueued int, tty bool) *InputLedger {
	return &InputLedger{maxQueued: maxQueued, tty: tty, entries: map[int64]InputRecord{}}
}
func (l *InputLedger) AcceptChars(offset int64, b []byte) (InputResult, error) {
	if l.closed {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_closed")
	}
	if len(b) == 0 {
		return InputResult{NextOffset: l.next}, fmt.Errorf("empty input")
	}
	rec := InputRecord{Kind: "chars", Offset: offset, Length: len(b), Hash: sha256.Sum256(b)}
	if old, ok := l.entries[offset]; ok {
		if old == rec {
			return InputResult{Record: old, NextOffset: l.next, Duplicate: true}, nil
		}
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_conflict")
	}
	if offset < l.next {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_conflict")
	}
	if offset > l.next {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_gap")
	}
	if l.queued+len(b) > l.maxQueued {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_backpressure")
	}
	l.entries[offset] = rec
	l.next += int64(len(b))
	l.queued += len(b)
	return InputResult{Record: rec, AcceptedBytes: len(b), NextOffset: l.next}, nil
}
func (l *InputLedger) AcceptEOF(offset int64) (InputResult, error) {
	if l.tty {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_eof_unsupported")
	}
	rec := InputRecord{Kind: "eof", Offset: offset}
	if old, ok := l.entries[offset]; ok && old == rec {
		return InputResult{Record: old, NextOffset: l.next, Duplicate: true}, nil
	}
	if l.closed {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_closed")
	}
	if offset < l.next {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_conflict")
	}
	if offset > l.next {
		return InputResult{NextOffset: l.next}, fmt.Errorf("input_gap")
	}
	l.entries[offset] = rec
	l.closed = true
	return InputResult{Record: rec, NextOffset: l.next}, nil
}
func (l *InputLedger) Delivered(n int) {
	l.queued -= n
	if l.queued < 0 {
		l.queued = 0
	}
}
func (l *InputLedger) NextOffset() int64 { return l.next }
