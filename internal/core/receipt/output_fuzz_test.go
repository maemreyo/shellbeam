package receipt

import (
	"testing"
	"unicode/utf8"
)

func FuzzVisibleOutput(f *testing.F) {
	f.Add([]byte("a€b"), 2)
	f.Add([]byte{0xff, 0xfe}, 1)
	f.Fuzz(func(t *testing.T, b []byte, max int) {
		if max < 0 {
			max = -max
		}
		if max > len(b)+4 {
			max = len(b) + 4
		}
		text, n, _ := VisibleOutput(b, max)
		if n < 0 || n > len(b) || n > max {
			t.Fatalf("n=%d len=%d max=%d", n, len(b), max)
		}
		if !utf8.ValidString(text) {
			t.Fatal("invalid visible utf8")
		}
	})
}
