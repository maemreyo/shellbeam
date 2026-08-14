package workspace

import "testing"

func TestSelectionVocabularyValidate(t *testing.T) {
	for _, basis := range []SelectionBasis{SelectionWorkspaceDirty, SelectionActivityDelta} {
		if err := basis.Validate(); err != nil {
			t.Fatalf("basis %q: %v", basis, err)
		}
	}
	if err := SelectionBasis("mystery").Validate(); err == nil {
		t.Fatal("unknown selection basis accepted")
	}

	if err := SampleFreshlySampled.Validate(); err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if err := SampleFreshness("cached").Validate(); err == nil {
		t.Fatal("unknown sample freshness accepted")
	}

	for _, completeness := range []SelectionCompleteness{
		SelectionComplete,
		SelectionPartial,
		SelectionDiverged,
		SelectionPotentiallyStale,
		SelectionUnavailable,
	} {
		if err := completeness.Validate(); err != nil {
			t.Fatalf("completeness %q: %v", completeness, err)
		}
	}
	if err := SelectionCompleteness("exact").Validate(); err == nil {
		t.Fatal("unknown completeness accepted")
	}
}

func TestSelectionChangeRecordValidate(t *testing.T) {
	valid := []ChangeRecord{
		{PathTransition: PathAdded, NewPath: "pkg/new.go", SourceTransition: SourceAvailabilityChanged, VCSTransition: VCSOther, Untracked: true},
		{PathTransition: PathModified, NewPath: "pkg/existing.go", SourceTransition: SourceBytesChanged, VCSTransition: VCSIndex},
		{PathTransition: PathDeleted, OldPath: "pkg/old.go", SourceTransition: SourceAvailabilityChanged, VCSTransition: VCSHead},
		{PathTransition: PathReplaced, OldPath: "old/name.go", NewPath: "new/name.go", SourceTransition: SourceIdentityChanged, VCSTransition: VCSOther},
		{PathTransition: PathNone, SourceTransition: SourceUnchanged, VCSTransition: VCSRef},
	}
	for i, record := range valid {
		if err := record.Validate(); err != nil {
			t.Fatalf("valid case %d: %v (%#v)", i, err, record)
		}
	}

	invalid := []ChangeRecord{
		{PathTransition: PathTransition("renamed"), OldPath: "a.go", NewPath: "b.go", SourceTransition: SourceIdentityChanged, VCSTransition: VCSOther},
		{PathTransition: PathAdded, NewPath: "/absolute.go", SourceTransition: SourceAvailabilityChanged, VCSTransition: VCSOther},
		{PathTransition: PathModified, SourceTransition: SourceBytesChanged, VCSTransition: VCSIndex},
		{PathTransition: PathReplaced, OldPath: "same.go", NewPath: "same.go", SourceTransition: SourceIdentityChanged, VCSTransition: VCSOther},
		{PathTransition: PathAdded, OldPath: "old.go", NewPath: "new.go", SourceTransition: SourceAvailabilityChanged, VCSTransition: VCSOther},
		{PathTransition: PathNone, NewPath: "unexpected.go", SourceTransition: SourceUnchanged, VCSTransition: VCSNone},
		{PathTransition: PathAdded, NewPath: "new.go", SourceTransition: SourceTransition("semantic"), VCSTransition: VCSOther},
		{PathTransition: PathAdded, NewPath: "new.go", SourceTransition: SourceAvailabilityChanged, VCSTransition: VCSTransition("network")},
	}
	for i, record := range invalid {
		if err := record.Validate(); err == nil {
			t.Fatalf("invalid case %d accepted: %#v", i, record)
		}
	}
}
