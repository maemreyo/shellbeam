package codeintel

import (
	"fmt"
	"unicode/utf8"
)

type displayLine struct {
	start int
	end   int
	next  int
}

func DisplayPositionToByteOffset(src []byte, line, column int) (int64, error) {
	if !utf8.Valid(src) {
		return 0, fmt.Errorf("invalid utf-8 source")
	}
	if line < 1 || column < 1 {
		return 0, fmt.Errorf("invalid display position")
	}
	lines := displayLines(src)
	if line > len(lines) {
		return 0, fmt.Errorf("display line out of range")
	}
	span := lines[line-1]
	offset := span.start
	currentColumn := 1
	for offset < span.end {
		if currentColumn == column {
			return int64(offset), nil
		}
		_, size := utf8.DecodeRune(src[offset:span.end])
		offset += size
		currentColumn++
	}
	if currentColumn == column {
		return int64(span.end), nil
	}
	return 0, fmt.Errorf("display column out of range")
}

func ByteOffsetToDisplayPosition(src []byte, offset int64) (line, column int, err error) {
	if !utf8.Valid(src) {
		return 0, 0, fmt.Errorf("invalid utf-8 source")
	}
	if offset < 0 || offset > int64(len(src)) {
		return 0, 0, fmt.Errorf("byte offset out of range")
	}
	want := int(offset)
	for i, span := range displayLines(src) {
		if want < span.start {
			break
		}
		if want <= span.end {
			if !utf8.Valid(src[span.start:want]) {
				return 0, 0, fmt.Errorf("byte offset is not a utf-8 boundary")
			}
			return i + 1, utf8.RuneCount(src[span.start:want]) + 1, nil
		}
		if want < span.next {
			return 0, 0, fmt.Errorf("byte offset is inside a line separator")
		}
	}
	return 0, 0, fmt.Errorf("byte offset is not addressable")
}

func displayLines(src []byte) []displayLine {
	lines := make([]displayLine, 0, 16)
	start := 0
	for i := 0; i < len(src); {
		if src[i] != '\n' {
			_, size := utf8.DecodeRune(src[i:])
			i += size
			continue
		}
		end := i
		if end > start && src[end-1] == '\r' {
			end--
		}
		lines = append(lines, displayLine{start: start, end: end, next: i + 1})
		start = i + 1
		i++
	}
	lines = append(lines, displayLine{start: start, end: len(src), next: len(src)})
	return lines
}
