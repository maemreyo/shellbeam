package capability

func (c Catalog) WithEnvironmentObservation(maxVariables, maxProbes int, probeTimeoutMS int64, probeOutputBytes, cacheEntries int, probeIDs []string) Catalog {
	out := c.Clone()
	if maxVariables < 1 || maxProbes < 1 || probeTimeoutMS < 1 || probeOutputBytes < 1 || cacheEntries < 1 || len(probeIDs) == 0 || len(probeIDs) > maxProbes {
		return out
	}
	seen := make(map[string]struct{}, len(probeIDs))
	for _, probeID := range probeIDs {
		if probeID == "" {
			return out
		}
		if _, exists := seen[probeID]; exists {
			return out
		}
		seen[probeID] = struct{}{}
	}
	out.Features[FeatureEnvironmentFingerprint] = Available
	out.EnvironmentSnapshotSchemaVersions = []int{1}
	out.EnvironmentFingerprintVersions = []int{1}
	out.ToolchainFingerprintVersions = []int{1}
	out.EnvironmentToolchainProbeIDs = append([]string(nil), probeIDs...)
	out.Limits.EnvironmentRelevantVariables = maxVariables
	out.Limits.EnvironmentToolchainProbes = maxProbes
	out.Limits.EnvironmentProbeTimeoutMS = probeTimeoutMS
	out.Limits.EnvironmentProbeOutputBytes = probeOutputBytes
	out.Limits.EnvironmentCacheEntries = cacheEntries
	return out
}

func (c Catalog) WithProcessInspection(maxDescendants, maxDepth, maxBytes int, maxMS int64, maxPorts int, portSupported bool) Catalog {
	out := c.Clone()
	if maxDescendants < 1 || maxDepth < 1 || maxBytes < 1 || maxMS < 1 || maxPorts < 1 {
		return out
	}
	out.Features[FeatureProcessInspection] = Available
	out.ProcessObservationSchemaVersions = []int{1}
	out.PortObservationSupported = portSupported
	out.Limits.ProcessDescendants = maxDescendants
	out.Limits.ProcessTraversalDepth = maxDepth
	out.Limits.ProcessObservationBytes = maxBytes
	out.Limits.ProcessObservationMS = maxMS
	out.Limits.ProcessPortRecords = maxPorts
	return out
}
