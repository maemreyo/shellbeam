package capability

func (c Catalog) WithTypedProjectCommands(packageProviders []string) Catalog {
	out := c.Clone()
	if len(packageProviders) == 0 {
		return out
	}
	for _, provider := range packageProviders {
		if provider == "" {
			return out
		}
	}
	out.Features[FeatureTypedProjectCommands] = Available
	out.TypedCommandVersions = []int{1}
	out.TypedCommandManifestVersion = 2
	out.TypedCommandParameterKinds = []string{"string", "enum", "integer", "repo_path", "repo_package"}
	out.TypedCommandPackageProviders = append([]string(nil), packageProviders...)
	foundV3 := false
	for _, version := range out.ReceiptSchemaVersions {
		foundV3 = foundV3 || version == 3
	}
	if !foundV3 {
		out.ReceiptSchemaVersions = append(out.ReceiptSchemaVersions, 3)
	}
	return out
}

func (c Catalog) WithDelegatedInteractive(support DelegatedInteractiveSupport) Catalog {
	out := c.Clone()
	if support.ProviderID == "" || support.ProviderVersion < 1 || support.Platform == "" || support.MaxMutationRecords < 1 {
		return out
	}
	out.Features[FeatureDelegatedInteractive] = Available
	copy := support
	out.DelegatedInteractive = &copy
	foundV5 := false
	for _, version := range out.ReceiptSchemaVersions {
		foundV5 = foundV5 || version == 5
	}
	if !foundV5 {
		out.ReceiptSchemaVersions = append(out.ReceiptSchemaVersions, 5)
	}
	return out
}

func (c Catalog) WithInteractiveHandoff(support InteractiveHandoffSupport) Catalog {
	out := c.Clone()
	if (!support.ValidH2() && !support.ValidH4()) || out.Features[FeatureDelegatedInteractive] != Available || out.DelegatedInteractive == nil {
		return out
	}
	out.Features[FeatureInteractiveHandoff] = Available
	copy := support.Clone()
	out.InteractiveHandoff = &copy
	return out
}

func (c Catalog) WithReproductionCapsules(maxCapsules, maxReferences, metadataBytes int) Catalog {
	out := c.Clone()
	if maxCapsules < 1 || maxReferences < 1 || metadataBytes < 1 {
		return out
	}
	out.Features[FeatureReproductionCapsules] = Available
	out.ReproSchemaVersions = []int{1}
	out.Limits.ReproMaxCapsules = maxCapsules
	out.Limits.ReproMaxReferences = maxReferences
	out.Limits.ReproMetadataBytes = metadataBytes
	return out
}
