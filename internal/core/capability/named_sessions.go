package capability

import persistentsession "github.com/maemreyo/shellbeam/internal/core/persistentsession"

func (c Catalog) WithNamedSessions(maxPersistentSessions int, maxSpoolBytes int64, maxQueuedInputBytes int) Catalog {
	out := c.Clone()
	if maxPersistentSessions < 1 || c.Limits.LiveSessions < 1 || maxPersistentSessions > c.Limits.LiveSessions {
		return out
	}
	if maxSpoolBytes < 1 || c.Limits.SessionOutputBytes < 1 || maxSpoolBytes > c.Limits.SessionOutputBytes || maxQueuedInputBytes < 1 {
		return out
	}
	out.Features[FeatureNamedSessions] = Available
	foundReceiptV4 := false
	for _, version := range out.ReceiptSchemaVersions {
		foundReceiptV4 = foundReceiptV4 || version == 4
	}
	if !foundReceiptV4 {
		out.ReceiptSchemaVersions = append(out.ReceiptSchemaVersions, 4)
	}
	out.PersistentSessionSchemaVersions = []int{persistentsession.SchemaVersion}
	out.SupervisorProtocolVersions = []int{persistentsession.ProtocolVersion}
	out.PersistentNonTTY = true
	out.PersistentTTY = false
	out.PersistentContinuity = persistentsession.ContinuityDaemonRestart
	out.HostRebootContinuity = false
	out.Limits.PersistentSessions = maxPersistentSessions
	out.Limits.PersistentSessionNameBytes = persistentsession.MaxSessionNameBytes
	out.Limits.PersistentSessionInspectRows = persistentsession.MaxInspectRows
	out.Limits.PersistentSessionInspectDefaultRows = persistentsession.DefaultInspectRows
	out.Limits.PersistentInputRecords = persistentsession.MaxInputRecords
	out.Limits.PersistentInputRecordMetadataBytes = persistentsession.MaxInputRecordMetadataBytes
	out.Limits.PersistentKillRecords = persistentsession.MaxKillRecords
	out.Limits.PersistentRecoverySpoolBytes = maxSpoolBytes
	out.Limits.PersistentQueuedInputBytes = maxQueuedInputBytes
	out.Limits.PersistentReattachHandshakeTimeoutMS = persistentsession.ReattachHandshakeTimeoutMS
	out.Limits.PersistentStartupReattachConcurrency = persistentsession.StartupReattachConcurrency
	out.Limits.PersistentStartupReattachBudgetMS = persistentsession.StartupReattachBudgetMS
	return out
}
