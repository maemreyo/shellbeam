package codeintel

import "testing"

func TestDisplayPositionToByteOffsetUsesUnicodeScalars(t *testing.T) {
	src := []byte("ascii\nTiếng Việt 😀\na\u0301b\r\nlast")
	cases := []struct {
		name         string
		line, column int
		want         int64
	}{
		{name: "start", line: 1, column: 1, want: 0},
		{name: "ascii end", line: 1, column: 6, want: 5},
		{name: "second line start", line: 2, column: 1, want: 6},
		{name: "after T", line: 2, column: 2, want: 7},
		{name: "after Vietnamese scalar", line: 2, column: 3, want: 8},
		{name: "after emoji", line: 2, column: 13, want: 25},
		{name: "combining mark is scalar", line: 3, column: 3, want: 29},
		{name: "crlf line end", line: 3, column: 4, want: 30},
		{name: "after crlf", line: 4, column: 1, want: 32},
		{name: "eof", line: 4, column: 5, want: 36},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DisplayPositionToByteOffset(src, tc.line, tc.column)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if got != tc.want {
				t.Fatalf("offset = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDisplayPositionToByteOffsetAllowsTrailingEmptyLine(t *testing.T) {
	src := []byte("x\n")
	got, err := DisplayPositionToByteOffset(src, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(len(src)) {
		t.Fatalf("offset = %d, want %d", got, len(src))
	}
}

func TestByteOffsetToDisplayPositionRoundTripsAddressableOffsets(t *testing.T) {
	src := []byte("a😀\nTiếng\r\nz")
	cases := []struct {
		offset       int64
		line, column int
	}{
		{offset: 0, line: 1, column: 1},
		{offset: 1, line: 1, column: 2},
		{offset: 5, line: 1, column: 3},
		{offset: 6, line: 2, column: 1},
		{offset: 7, line: 2, column: 2},
		{offset: 8, line: 2, column: 3},
		{offset: 11, line: 2, column: 4},
		{offset: 13, line: 2, column: 6},
		{offset: 15, line: 3, column: 1},
		{offset: 16, line: 3, column: 2},
	}
	for _, tc := range cases {
		line, column, err := ByteOffsetToDisplayPosition(src, tc.offset)
		if err != nil {
			t.Fatalf("offset %d: %v", tc.offset, err)
		}
		if line != tc.line || column != tc.column {
			t.Fatalf("offset %d => %d:%d, want %d:%d", tc.offset, line, column, tc.line, tc.column)
		}
		got, err := DisplayPositionToByteOffset(src, line, column)
		if err != nil {
			t.Fatalf("roundtrip %d:%d: %v", line, column, err)
		}
		if got != tc.offset {
			t.Fatalf("roundtrip = %d, want %d", got, tc.offset)
		}
	}
}

func TestPositionConversionRejectsInvalidCoordinatesAndEncoding(t *testing.T) {
	valid := []byte("a😀\r\nb")
	for _, tc := range []struct {
		line, column int
	}{
		{0, 1},
		{1, 0},
		{1, 4},
		{3, 1},
	} {
		if _, err := DisplayPositionToByteOffset(valid, tc.line, tc.column); err == nil {
			t.Fatalf("accepted invalid display position %d:%d", tc.line, tc.column)
		}
	}
	for _, offset := range []int64{-1, 2, 3, 4, 6, int64(len(valid)) + 1} {
		if _, _, err := ByteOffsetToDisplayPosition(valid, offset); err == nil {
			t.Fatalf("accepted invalid/non-addressable offset %d", offset)
		}
	}

	invalidUTF8 := []byte{0xff, 0x78}
	if _, err := DisplayPositionToByteOffset(invalidUTF8, 1, 1); err == nil {
		t.Fatal("display conversion accepted invalid utf-8")
	}
	if _, _, err := ByteOffsetToDisplayPosition(invalidUTF8, 0); err == nil {
		t.Fatal("byte conversion accepted invalid utf-8")
	}
}
