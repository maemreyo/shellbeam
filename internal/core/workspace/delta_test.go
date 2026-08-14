package workspace

import (
	"testing"
	"time"
)

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

func TestDeltaLimitsNormalizeAndValidate(t *testing.T) {
	got := (DeltaLimits{}).Normalize()
	if got.MaxPaths != 256 || got.MaxOutputBytes != 256<<10 || got.TimeoutMS != 150 || got.RequireComplete {
		t.Fatalf("defaults=%#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []DeltaLimits{{MaxPaths: -1}, {MaxPaths: 4097}, {MaxOutputBytes: (1 << 20) + 1}, {TimeoutMS: 5001}} {
		if err := invalid.Normalize().Validate(); err == nil {
			t.Fatalf("invalid limits accepted: %#v", invalid)
		}
	}
}

func TestDeltaSampleResolvedPathsAreSafeAndUnique(t *testing.T) {
	now := time.Now().UTC()
	barrier := CoherenceBarrier{DaemonIncarnation: "d"}
	base := DeltaSample{SchemaVersion: DeltaSampleSchemaVersion, RepositoryID: RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: WorkspaceID("ws_01K00000000000000000000000"), Freshness: SampleFreshlySampled, Completeness: SelectionComplete, ObservedAt: now, BarrierBefore: barrier, BarrierAfter: barrier, CacheEligible: true}
	base.ResolvedPaths = []string{"pkg/restored.go"}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid resolved path: %v", err)
	}
	for _, paths := range [][]string{{"../escape.go"}, {"/abs.go"}, {"same.go", "same.go"}} {
		bad := base
		bad.ResolvedPaths = paths
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid resolved paths accepted: %#v", paths)
		}
	}
}

func TestDeltaSelectionModesPreventFalseCompleteClaim(t *testing.T) {
	base := validDeltaSampleForTest()
	base.SelectionModes = SelectionModeQuality{SparseCheckout: ModeUnknown, AssumeUnchanged: ModePresent}
	if err := base.Validate(); err == nil {
		t.Fatal("known assume-unchanged mode accepted as complete")
	}
	base.Completeness = SelectionPartial
	if err := base.Validate(); err != nil {
		t.Fatalf("partial mode-aware sample: %v", err)
	}
	base.SelectionModes = SelectionModeQuality{SparseCheckout: ModeUnknown, AssumeUnchanged: ModeUnknown}
	base.Completeness = SelectionComplete
	if err := base.Validate(); err != nil {
		t.Fatalf("unknown modes should not invent incompleteness: %v", err)
	}
}

func validDeltaSampleForTest() DeltaSample {
	now := time.Now().UTC()
	barrier := CoherenceBarrier{DaemonIncarnation: "d"}
	return DeltaSample{SchemaVersion: DeltaSampleSchemaVersion, RepositoryID: RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: WorkspaceID("ws_01K00000000000000000000000"), Freshness: SampleFreshlySampled, Completeness: SelectionComplete, ObservedAt: now, BarrierBefore: barrier, BarrierAfter: barrier, CacheEligible: true}
}
