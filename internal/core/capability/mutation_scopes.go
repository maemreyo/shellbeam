package capability

const mutationScopeMinTTLMS int64 = 1000

func (c Catalog) WithMutationScopes(maxActivity, maxWorkspace, maxPaths, maxSelectorBytes, maxAdvisories int, defaultTTLMS, maxTTLMS int64) Catalog {
	out := c.Clone()
	if maxActivity < 1 || maxWorkspace < maxActivity || maxPaths < 1 || maxSelectorBytes < 1 || maxAdvisories < 1 || defaultTTLMS < mutationScopeMinTTLMS || maxTTLMS < defaultTTLMS {
		return out
	}
	out.Features[FeatureMutationScopes] = Available
	out.MutationScopeSchemaVersions = []int{1}
	out.Limits.MutationScopeActivePerActivity = maxActivity
	out.Limits.MutationScopeActivePerWorkspace = maxWorkspace
	out.Limits.MutationScopePathsPerScope = maxPaths
	out.Limits.MutationScopeSelectorBytes = maxSelectorBytes
	out.Limits.MutationScopeAdvisories = maxAdvisories
	out.Limits.MutationScopeMinTTLMS = mutationScopeMinTTLMS
	out.Limits.MutationScopeDefaultTTLMS = defaultTTLMS
	out.Limits.MutationScopeMaxTTLMS = maxTTLMS
	return out
}
