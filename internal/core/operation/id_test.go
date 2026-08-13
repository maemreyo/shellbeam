package operation

import "testing"

func TestParseID(t *testing.T) {
	for _, v := range []string{"a", "A_1-x", "x" + string(make([]byte, 0))} {
		if _, err := ParseID(v); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
	for _, v := range []string{"", "-x", "a/b", "hello world"} {
		if _, err := ParseID(v); err == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
