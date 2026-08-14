package observation

// Snapshot recovery is intentionally delegated to SnapshotProvider. The provider
// must return bounded current facts and the exact observation cut captured with
// those facts; this package never fabricates a cut from unrelated reads.
