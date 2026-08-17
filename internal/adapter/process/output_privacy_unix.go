//go:build linux || darwin

package process

import (
	"bytes"
	"sort"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const traceOutputRedaction = "[redacted]"

type traceOutputRedactor struct {
	patterns   [][]byte
	maxPattern int
	pending    []byte
}

func newTraceOutputRedactor(additions []operation.EnvironmentEntry) *traceOutputRedactor {
	patterns := make(map[string]struct{})
	for _, addition := range additions {
		if _, ok := traceEnvironmentKeys[addition.Key]; !ok {
			continue
		}
		patterns[addition.Key] = struct{}{}
		switch addition.Key {
		case "DYLD_INSERT_LIBRARIES", "SHELLBEAM_TRACE_SOCKET":
			patterns[addition.Value] = struct{}{}
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(patterns))
	maxPattern := 0
	for pattern := range patterns {
		if pattern == "" {
			continue
		}
		b := []byte(pattern)
		out = append(out, b)
		if len(b) > maxPattern {
			maxPattern = len(b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	if len(out) == 0 {
		return nil
	}
	return &traceOutputRedactor{patterns: out, maxPattern: maxPattern}
}

func (r *traceOutputRedactor) Push(chunk []byte) []byte {
	return r.consume(chunk, false)
}

func (r *traceOutputRedactor) Flush() []byte {
	return r.consume(nil, true)
}

func (r *traceOutputRedactor) consume(chunk []byte, final bool) []byte {
	if r == nil {
		return append([]byte(nil), chunk...)
	}
	buf := append(r.pending, chunk...)
	limit := len(buf)
	if !final && r.maxPattern > 1 {
		limit -= r.maxPattern - 1
		if limit < 0 {
			limit = 0
		}
	}
	out := make([]byte, 0, len(buf))
	i := 0
	for i < limit {
		if pattern := r.match(buf[i:]); pattern != nil {
			out = append(out, traceOutputRedaction...)
			i += len(pattern)
			continue
		}
		out = append(out, buf[i])
		i++
	}
	r.pending = append(r.pending[:0], buf[i:]...)
	return out
}

func (r *traceOutputRedactor) match(buf []byte) []byte {
	for _, pattern := range r.patterns {
		if len(buf) >= len(pattern) && bytes.Equal(buf[:len(pattern)], pattern) {
			return pattern
		}
	}
	return nil
}
