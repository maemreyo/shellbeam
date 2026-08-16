package capability

import checkpoint "github.com/maemreyo/shellbeam/internal/core/checkpoint"

type CheckpointSupport struct {
	SchemaVersions        []int                        `json:"schema_versions"`
	Provider              checkpoint.ProviderIdentity  `json:"provider"`
	ConflictDetection     checkpoint.ConflictDetection `json:"conflict_detection"`
	LocalSensitiveContent bool                         `json:"local_sensitive_content"`
}

func (c Catalog) WithSafetyCheckpoints(provider checkpoint.ProviderIdentity, matrix checkpoint.ConflictDetection) Catalog {
	out := c.Clone()
	if provider.Validate() != nil || matrix.Validate() != nil {
		return out
	}
	if matrix.RegularFile != checkpoint.ConflictBestEffort ||
		matrix.Symlink != checkpoint.ConflictBestEffort ||
		matrix.AbsentToFile != checkpoint.ConflictBestEffort ||
		matrix.DirectoryTree != checkpoint.ConflictUnsupported {
		return out
	}
	out.Features[FeatureSafetyCheckpoints] = Available
	out.SafetyCheckpoints = &CheckpointSupport{
		SchemaVersions:        []int{checkpoint.SchemaVersion},
		Provider:              provider,
		ConflictDetection:     matrix,
		LocalSensitiveContent: true,
	}
	out.Limits.CheckpointCreateSelectors = checkpoint.MaxCreateSelectors
	out.Limits.CheckpointSelectorBytes = checkpoint.MaxSelectorBytes
	out.Limits.CheckpointTotalSelectorBytes = checkpoint.MaxTotalSelectorBytes
	out.Limits.CheckpointWalkEntries = checkpoint.MaxWalkEntries
	out.Limits.CheckpointCapturedEntries = checkpoint.MaxCapturedEntries
	out.Limits.CheckpointRegularFileBytes = checkpoint.MaxRegularFileBytes
	out.Limits.CheckpointBytes = checkpoint.MaxCheckpointBytes
	out.Limits.CheckpointRetained = checkpoint.MaxRetainedCheckpoints
	out.Limits.CheckpointPrivateProviderBytes = checkpoint.MaxPrivateProviderBytes
	out.Limits.CheckpointRetentionAgeMS = checkpoint.MaxRetentionAge.Milliseconds()
	out.Limits.CheckpointRestorePaths = checkpoint.MaxRestorePaths
	out.Limits.CheckpointPublicEntryRefs = checkpoint.MaxPublicEntryRefs
	out.Limits.CheckpointPublicSummaries = checkpoint.MaxPublicSummaries
	return out
}
