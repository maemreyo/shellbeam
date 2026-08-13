package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReleaseEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := writeReleaseEvidence(path, "abc"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got releaseEvidence
	if json.Unmarshal(b, &got) != nil || got.SchemaVersion != 1 || got.SourceFingerprint != "abc" {
		t.Fatalf("%s", b)
	}
}
