package lsp

import (
	"testing"

	"go.lsp.dev/protocol"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

func TestPositionConversionRoundTripsNegotiatedEncodings(t *testing.T) {
	src := []byte("ASCII\r\nTiếng Việt\n🙂x\né\n")
	tests := []struct {
		name     string
		encoding protocol.PositionEncodingKind
		offset   int64
		want     protocol.Position
	}{
		{name: "utf8 emoji start", encoding: protocol.PositionEncodingKindUTF8, offset: int64(len("ASCII\r\nTiếng Việt\n")), want: protocol.Position{Line: 2, Character: 0}},
		{name: "utf8 after emoji", encoding: protocol.PositionEncodingKindUTF8, offset: int64(len("ASCII\r\nTiếng Việt\n🙂")), want: protocol.Position{Line: 2, Character: 4}},
		{name: "utf16 after emoji", encoding: protocol.PositionEncodingKindUTF16, offset: int64(len("ASCII\r\nTiếng Việt\n🙂")), want: protocol.Position{Line: 2, Character: 2}},
		{name: "utf32 after emoji", encoding: protocol.PositionEncodingKindUTF32, offset: int64(len("ASCII\r\nTiếng Việt\n🙂")), want: protocol.Position{Line: 2, Character: 1}},
		{name: "combining second scalar", encoding: protocol.PositionEncodingKindUTF16, offset: int64(len("ASCII\r\nTiếng Việt\n🙂x\ne")), want: protocol.Position{Line: 3, Character: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			position, err := ByteOffsetToPosition(src, tt.offset, tt.encoding)
			if err != nil {
				t.Fatal(err)
			}
			if position != tt.want {
				t.Fatalf("position=%+v want=%+v", position, tt.want)
			}
			offset, err := PositionToByteOffset(src, tt.want, tt.encoding)
			if err != nil {
				t.Fatal(err)
			}
			if offset != tt.offset {
				t.Fatalf("offset=%d want=%d", offset, tt.offset)
			}
		})
	}
}

func TestPositionEncodingDefaultsOnlyWhenServerOmittedIt(t *testing.T) {
	if got := EffectivePositionEncoding(""); got != protocol.PositionEncodingKindUTF16 {
		t.Fatalf("default encoding=%q", got)
	}
	for _, encoding := range []protocol.PositionEncodingKind{
		protocol.PositionEncodingKindUTF8,
		protocol.PositionEncodingKindUTF16,
		protocol.PositionEncodingKindUTF32,
	} {
		if got := EffectivePositionEncoding(encoding); got != encoding {
			t.Fatalf("encoding=%q got=%q", encoding, got)
		}
	}
}

func TestPositionConversionRejectsInvalidBoundariesAndCoordinates(t *testing.T) {
	src := []byte("🙂x\r\nTiếng\n")
	cases := []struct {
		name string
		fn   func() error
	}{
		{name: "byte offset inside utf8 rune", fn: func() error {
			_, err := ByteOffsetToPosition(src, 1, protocol.PositionEncodingKindUTF8)
			return err
		}},
		{name: "utf8 character inside rune", fn: func() error {
			_, err := PositionToByteOffset(src, protocol.Position{Line: 0, Character: 1}, protocol.PositionEncodingKindUTF8)
			return err
		}},
		{name: "utf16 inside surrogate pair", fn: func() error {
			_, err := PositionToByteOffset(src, protocol.Position{Line: 0, Character: 1}, protocol.PositionEncodingKindUTF16)
			return err
		}},
		{name: "offset inside crlf separator", fn: func() error {
			_, err := ByteOffsetToPosition(src, int64(len("🙂x\r")), protocol.PositionEncodingKindUTF8)
			return err
		}},
		{name: "line out of range", fn: func() error {
			_, err := PositionToByteOffset(src, protocol.Position{Line: 9}, protocol.PositionEncodingKindUTF16)
			return err
		}},
		{name: "unsupported encoding", fn: func() error {
			_, err := PositionToByteOffset(src, protocol.Position{}, protocol.PositionEncodingKind("utf-7"))
			return err
		}},
		{name: "invalid utf8 source", fn: func() error {
			_, err := PositionToByteOffset([]byte{0xff}, protocol.Position{}, protocol.PositionEncodingKindUTF8)
			return err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Fatal("invalid position unexpectedly accepted")
			}
		})
	}
}

func TestByteRangeAndLSPRangeRoundTripExactBytes(t *testing.T) {
	src := []byte("a🙂b\nTiếng\n")
	input := core.ByteRange{Start: 1, End: 5}
	for _, encoding := range []protocol.PositionEncodingKind{
		protocol.PositionEncodingKindUTF8,
		protocol.PositionEncodingKindUTF16,
		protocol.PositionEncodingKindUTF32,
	} {
		value, err := ByteRangeToRange(src, input, encoding)
		if err != nil {
			t.Fatal(err)
		}
		got, err := RangeToByteRange(src, value, encoding)
		if err != nil {
			t.Fatal(err)
		}
		if got != input {
			t.Fatalf("encoding=%q range=%+v want=%+v", encoding, got, input)
		}
	}
}

func TestPositionConversionAddressesEOFDeterministically(t *testing.T) {
	src := []byte("x\n")
	position, err := ByteOffsetToPosition(src, int64(len(src)), protocol.PositionEncodingKindUTF16)
	if err != nil {
		t.Fatal(err)
	}
	if position != (protocol.Position{Line: 1, Character: 0}) {
		t.Fatalf("EOF position=%+v", position)
	}
	offset, err := PositionToByteOffset(src, position, protocol.PositionEncodingKindUTF16)
	if err != nil {
		t.Fatal(err)
	}
	if offset != int64(len(src)) {
		t.Fatalf("EOF offset=%d", offset)
	}
}
