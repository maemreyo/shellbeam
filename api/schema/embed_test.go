package schema

import "testing"

func TestLoadRejectsUnknown(t *testing.T) {
	if _, err := Load("other.json"); err == nil {
		t.Fatal("expected error")
	}
}
