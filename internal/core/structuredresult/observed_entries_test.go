package structuredresult

import (
	"strings"
	"testing"
)

func TestObservedEntryCountsValidateIndependentFileAndEntryCounts(t *testing.T) {
	valid := []ObservedEntryCounts{
		{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 1, Fail: 1},
		{Namespace: "vitest", VocabularyVersion: 1},
		{Namespace: "jest", VocabularyVersion: 1, Files: 1, Entries: 1, Error: 1},
	}
	for _, counts := range valid {
		if err := counts.Validate(); err != nil {
			t.Fatalf("valid counts rejected: %#v err=%v", counts, err)
		}
	}
}

func TestObservedEntryCountsRejectInvalidVocabularyBoundsAndArithmetic(t *testing.T) {
	valid := ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 3, Pass: 1, Fail: 1, Skip: 1}
	cases := []struct {
		name   string
		mutate func(*ObservedEntryCounts)
	}{
		{"blank namespace", func(v *ObservedEntryCounts) { v.Namespace = "" }},
		{"control namespace", func(v *ObservedEntryCounts) { v.Namespace = "jest\n" }},
		{"oversized namespace", func(v *ObservedEntryCounts) { v.Namespace = strings.Repeat("x", 129) }},
		{"wrong vocabulary", func(v *ObservedEntryCounts) { v.VocabularyVersion = 2 }},
		{"negative files", func(v *ObservedEntryCounts) { v.Files = -1 }},
		{"negative entry", func(v *ObservedEntryCounts) { v.Entries = -1 }},
		{"negative status", func(v *ObservedEntryCounts) { v.Fail = -1 }},
		{"status sum mismatch", func(v *ObservedEntryCounts) { v.Pass = 0 }},
		{"files above ceiling", func(v *ObservedEntryCounts) { v.Files = MaxObservedEntries + 1 }},
		{"entries above ceiling", func(v *ObservedEntryCounts) {
			v.Entries = MaxObservedEntries + 1
			v.Pass = MaxObservedEntries - 1
			v.Fail = 1
			v.Skip = 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid counts accepted: %#v", got)
			}
		})
	}
}
