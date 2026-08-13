package receipt

import "testing"

func TestVisibleOutputPreservesUTF8Boundary(t *testing.T) {
	raw := []byte("a€b")
	text, n, truncated := VisibleOutput(raw, 2)
	if text != "a" || n != 1 || !truncated {
		t.Fatalf("%q %d %v", text, n, truncated)
	}
	text, n, truncated = VisibleOutput(raw, 4)
	if text != "a€" || n != 4 || !truncated {
		t.Fatalf("%q %d %v", text, n, truncated)
	}
}
func TestVisibleOutputReplacesInvalid(t *testing.T) {
	text, n, _ := VisibleOutput([]byte{0xff, 'x'}, 2)
	if text != "�x" || n != 2 {
		t.Fatalf("%q %d", text, n)
	}
}
