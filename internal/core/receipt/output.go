package receipt

import (
	"strings"
	"unicode/utf8"
)

// VisibleOutput returns valid model-visible text and raw bytes consumed.
func VisibleOutput(raw []byte, max int) (string, int, bool) {
	if max < 0 {
		max = 0
	}
	n := min(len(raw), max)
	if n < len(raw) {
		for n > 0 && !utf8.Valid(raw[:n]) {
			// Keep invalid bytes (which are replaced), but back off only when the suffix is an incomplete valid rune.
			start := n - 1
			for start > 0 && raw[start]&0xc0 == 0x80 {
				start--
			}
			if utf8.FullRune(raw[start:n]) {
				break
			}
			n = start
		}
	}
	return strings.ToValidUTF8(string(raw[:n]), "�"), n, n < len(raw)
}
