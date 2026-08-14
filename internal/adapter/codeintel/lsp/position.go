package lsp

import (
	"fmt"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

type sourceLine struct {
	start int
	end   int
	next  int
}

func EffectivePositionEncoding(encoding protocol.PositionEncodingKind) protocol.PositionEncodingKind {
	if encoding == "" {
		return protocol.PositionEncodingKindUTF16
	}
	return encoding
}

func ByteOffsetToPosition(src []byte, offset int64, encoding protocol.PositionEncodingKind) (protocol.Position, error) {
	if !utf8.Valid(src) {
		return protocol.Position{}, fmt.Errorf("invalid UTF-8 source")
	}
	if offset < 0 || offset > int64(len(src)) {
		return protocol.Position{}, fmt.Errorf("byte offset out of range")
	}
	encoding = EffectivePositionEncoding(encoding)
	if !supportedPositionEncoding(encoding) {
		return protocol.Position{}, fmt.Errorf("unsupported position encoding %q", encoding)
	}
	want := int(offset)
	for lineNumber, span := range sourceLines(src) {
		if want < span.start {
			break
		}
		if want <= span.end {
			if !utf8.Valid(src[span.start:want]) {
				return protocol.Position{}, fmt.Errorf("byte offset is not a UTF-8 boundary")
			}
			character, err := bytePrefixToCharacter(src[span.start:want], encoding)
			if err != nil {
				return protocol.Position{}, err
			}
			return protocol.Position{Line: uint32(lineNumber), Character: character}, nil
		}
		if want < span.next {
			return protocol.Position{}, fmt.Errorf("byte offset is inside a line separator")
		}
	}
	return protocol.Position{}, fmt.Errorf("byte offset is not addressable")
}

func PositionToByteOffset(src []byte, position protocol.Position, encoding protocol.PositionEncodingKind) (int64, error) {
	if !utf8.Valid(src) {
		return 0, fmt.Errorf("invalid UTF-8 source")
	}
	encoding = EffectivePositionEncoding(encoding)
	if !supportedPositionEncoding(encoding) {
		return 0, fmt.Errorf("unsupported position encoding %q", encoding)
	}
	lines := sourceLines(src)
	if uint64(position.Line) >= uint64(len(lines)) {
		return 0, fmt.Errorf("LSP line out of range")
	}
	span := lines[position.Line]
	within, err := characterToByteOffset(src[span.start:span.end], position.Character, encoding)
	if err != nil {
		return 0, err
	}
	return int64(span.start + within), nil
}

func ByteRangeToRange(src []byte, value core.ByteRange, encoding protocol.PositionEncodingKind) (protocol.Range, error) {
	if err := value.Validate(); err != nil {
		return protocol.Range{}, err
	}
	start, err := ByteOffsetToPosition(src, value.Start, encoding)
	if err != nil {
		return protocol.Range{}, err
	}
	end, err := ByteOffsetToPosition(src, value.End, encoding)
	if err != nil {
		return protocol.Range{}, err
	}
	return protocol.Range{Start: start, End: end}, nil
}

func RangeToByteRange(src []byte, value protocol.Range, encoding protocol.PositionEncodingKind) (core.ByteRange, error) {
	start, err := PositionToByteOffset(src, value.Start, encoding)
	if err != nil {
		return core.ByteRange{}, err
	}
	end, err := PositionToByteOffset(src, value.End, encoding)
	if err != nil {
		return core.ByteRange{}, err
	}
	result := core.ByteRange{Start: start, End: end}
	if err := result.Validate(); err != nil {
		return core.ByteRange{}, err
	}
	return result, nil
}

func sourceLines(src []byte) []sourceLine {
	lines := make([]sourceLine, 0, 16)
	start := 0
	for i, b := range src {
		if b != '\n' {
			continue
		}
		end := i
		if end > start && src[end-1] == '\r' {
			end--
		}
		lines = append(lines, sourceLine{start: start, end: end, next: i + 1})
		start = i + 1
	}
	lines = append(lines, sourceLine{start: start, end: len(src), next: len(src)})
	return lines
}

func bytePrefixToCharacter(prefix []byte, encoding protocol.PositionEncodingKind) (uint32, error) {
	switch encoding {
	case protocol.PositionEncodingKindUTF8:
		return uint32(len(prefix)), nil
	case protocol.PositionEncodingKindUTF32:
		return uint32(utf8.RuneCount(prefix)), nil
	case protocol.PositionEncodingKindUTF16:
		units := uint32(0)
		for len(prefix) > 0 {
			r, size := utf8.DecodeRune(prefix)
			if r == utf8.RuneError && size == 1 {
				return 0, fmt.Errorf("invalid UTF-8 source")
			}
			if r > 0xFFFF {
				units += 2
			} else {
				units++
			}
			prefix = prefix[size:]
		}
		return units, nil
	default:
		return 0, fmt.Errorf("unsupported position encoding %q", encoding)
	}
}

func characterToByteOffset(line []byte, character uint32, encoding protocol.PositionEncodingKind) (int, error) {
	if encoding == protocol.PositionEncodingKindUTF8 {
		if uint64(character) > uint64(len(line)) {
			return 0, fmt.Errorf("UTF-8 character out of range")
		}
		offset := int(character)
		if offset < len(line) && !utf8.RuneStart(line[offset]) {
			return 0, fmt.Errorf("UTF-8 character is inside a rune")
		}
		return offset, nil
	}
	units := uint32(0)
	for offset := 0; offset < len(line); {
		if units == character {
			return offset, nil
		}
		r, size := utf8.DecodeRune(line[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("invalid UTF-8 source")
		}
		increment := uint32(1)
		if encoding == protocol.PositionEncodingKindUTF16 && r > 0xFFFF {
			increment = 2
		}
		if units+increment > character {
			return 0, fmt.Errorf("LSP character is inside an encoded rune")
		}
		units += increment
		offset += size
	}
	if units == character {
		return len(line), nil
	}
	return 0, fmt.Errorf("LSP character out of range")
}

func supportedPositionEncoding(encoding protocol.PositionEncodingKind) bool {
	switch encoding {
	case protocol.PositionEncodingKindUTF8, protocol.PositionEncodingKindUTF16, protocol.PositionEncodingKindUTF32:
		return true
	default:
		return false
	}
}
