package capability

import (
	"reflect"
	"testing"

	checkpoint "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func TestE26SafetyCheckpointCapabilityIsExplicitVersionedAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureSafetyCheckpoints] != Unavailable || base.SafetyCheckpoints != nil {
		t.Fatalf("baseline leaked checkpoint support: %#v", base)
	}
	provider := checkpoint.ProviderIdentity{ID: "localfs", Version: 1}
	matrix := checkpoint.ConflictDetection{RegularFile: checkpoint.ConflictBestEffort, Symlink: checkpoint.ConflictBestEffort, AbsentToFile: checkpoint.ConflictBestEffort, DirectoryTree: checkpoint.ConflictUnsupported}
	got := base.WithSafetyCheckpoints(provider, matrix)
	if got.Features[FeatureSafetyCheckpoints] != Available || got.SafetyCheckpoints == nil {
		t.Fatalf("checkpoint capability unavailable: %#v", got)
	}
	if !reflect.DeepEqual(got.SafetyCheckpoints.SchemaVersions, []int{checkpoint.SchemaVersion}) || got.SafetyCheckpoints.Provider != provider || got.SafetyCheckpoints.ConflictDetection != matrix || !got.SafetyCheckpoints.LocalSensitiveContent {
		t.Fatalf("checkpoint support=%#v", got.SafetyCheckpoints)
	}
	limits := got.Limits
	if limits.CheckpointCreateSelectors != checkpoint.MaxCreateSelectors ||
		limits.CheckpointSelectorBytes != checkpoint.MaxSelectorBytes ||
		limits.CheckpointTotalSelectorBytes != checkpoint.MaxTotalSelectorBytes ||
		limits.CheckpointWalkEntries != checkpoint.MaxWalkEntries ||
		limits.CheckpointCapturedEntries != checkpoint.MaxCapturedEntries ||
		limits.CheckpointRegularFileBytes != checkpoint.MaxRegularFileBytes ||
		limits.CheckpointBytes != checkpoint.MaxCheckpointBytes ||
		limits.CheckpointRetained != checkpoint.MaxRetainedCheckpoints ||
		limits.CheckpointPrivateProviderBytes != checkpoint.MaxPrivateProviderBytes ||
		limits.CheckpointRetentionAgeMS != checkpoint.MaxRetentionAge.Milliseconds() ||
		limits.CheckpointRestorePaths != checkpoint.MaxRestorePaths ||
		limits.CheckpointPublicEntryRefs != checkpoint.MaxPublicEntryRefs ||
		limits.CheckpointPublicSummaries != checkpoint.MaxPublicSummaries {
		t.Fatalf("limits=%#v", limits)
	}
}

func TestE26SafetyCheckpointCapabilityRejectsInvalidProviderOrMatrix(t *testing.T) {
	base := Baseline(Limits{})
	valid := checkpoint.ConflictDetection{RegularFile: checkpoint.ConflictBestEffort, Symlink: checkpoint.ConflictBestEffort, AbsentToFile: checkpoint.ConflictBestEffort, DirectoryTree: checkpoint.ConflictUnsupported}
	if got := base.WithSafetyCheckpoints(checkpoint.ProviderIdentity{}, valid); got.Features[FeatureSafetyCheckpoints] != Unavailable || got.SafetyCheckpoints != nil {
		t.Fatal("invalid provider promoted checkpoint support")
	}
	valid.DirectoryTree = checkpoint.ConflictAtomicConditionalReplace
	if got := base.WithSafetyCheckpoints(checkpoint.ProviderIdentity{ID: "localfs", Version: 1}, valid); got.Features[FeatureSafetyCheckpoints] != Unavailable || got.SafetyCheckpoints != nil {
		t.Fatal("unsupported atomic guarantee promoted E26 v1")
	}
}

func TestE26SafetyCheckpointCloneDoesNotAliasSupport(t *testing.T) {
	matrix := checkpoint.ConflictDetection{RegularFile: checkpoint.ConflictBestEffort, Symlink: checkpoint.ConflictBestEffort, AbsentToFile: checkpoint.ConflictBestEffort, DirectoryTree: checkpoint.ConflictUnsupported}
	original := Baseline(Limits{}).WithSafetyCheckpoints(checkpoint.ProviderIdentity{ID: "localfs", Version: 1}, matrix)
	clone := original.Clone()
	clone.SafetyCheckpoints.SchemaVersions[0] = 99
	clone.SafetyCheckpoints.Provider.ID = "other"
	if original.SafetyCheckpoints.SchemaVersions[0] != checkpoint.SchemaVersion || original.SafetyCheckpoints.Provider.ID != "localfs" {
		t.Fatalf("clone aliased support: original=%#v clone=%#v", original.SafetyCheckpoints, clone.SafetyCheckpoints)
	}
}
