package main

import (
	"os"
	"strings"
	"testing"
)

func TestCandidateSourceContract(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, want := range []string{
		"//go:build goexperiment.jsonv2",
		`jsonv2 "encoding/json/v2"`,
		"jsonv2.RejectUnknownMembers(true)",
		"invalid-utf8", "duplicate-names", "unknown-names", "wrong-case", "trailing-json",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("candidate source missing %q", want)
		}
	}
}

func TestFixtureManifestExists(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schema_version": 1`) {
		t.Fatal("fixture schema version missing")
	}
}
