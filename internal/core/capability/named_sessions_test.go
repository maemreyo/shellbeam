package capability

import "testing"

func TestWithNamedSessionsAdvertisesExactB10Support(t *testing.T) {
	base := Baseline(Limits{LiveSessions: 32, SessionOutputBytes: 8 << 20})
	if base.Features[FeatureNamedSessions] != Unavailable {
		t.Fatal("named sessions unexpectedly available in baseline")
	}
	got := base.WithNamedSessions(16, 8<<20, 64<<10)
	if got.Features[FeatureNamedSessions] != Available {
		t.Fatal("named sessions not available")
	}
	foundV4 := false
	for _, version := range got.ReceiptSchemaVersions {
		foundV4 = foundV4 || version == 4
	}
	if !foundV4 {
		t.Fatalf("receipt versions=%v missing persistent v4", got.ReceiptSchemaVersions)
	}
	if len(got.PersistentSessionSchemaVersions) != 1 || got.PersistentSessionSchemaVersions[0] != 1 {
		t.Fatalf("persistent schema versions=%v", got.PersistentSessionSchemaVersions)
	}
	if len(got.SupervisorProtocolVersions) != 1 || got.SupervisorProtocolVersions[0] != 1 {
		t.Fatalf("supervisor protocol versions=%v", got.SupervisorProtocolVersions)
	}
	if !got.PersistentNonTTY || got.PersistentTTY || got.PersistentContinuity != "daemon_restart" || got.HostRebootContinuity {
		t.Fatalf("persistent support flags=%#v", got)
	}
	if got.Limits.PersistentSessions != 16 || got.Limits.PersistentSessionNameBytes != 128 || got.Limits.PersistentSessionInspectRows != 100 || got.Limits.PersistentSessionInspectDefaultRows != 25 {
		t.Fatalf("session limits=%#v", got.Limits)
	}
	if got.Limits.PersistentInputRecords != 4096 || got.Limits.PersistentInputRecordMetadataBytes != 1<<20 || got.Limits.PersistentKillRecords != 256 {
		t.Fatalf("history limits=%#v", got.Limits)
	}
	if got.Limits.PersistentRecoverySpoolBytes != 8<<20 || got.Limits.PersistentQueuedInputBytes != 64<<10 {
		t.Fatalf("byte limits=%#v", got.Limits)
	}
	if got.Limits.PersistentReattachHandshakeTimeoutMS != 2000 || got.Limits.PersistentStartupReattachConcurrency != 16 || got.Limits.PersistentStartupReattachBudgetMS != 5000 {
		t.Fatalf("reattach limits=%#v", got.Limits)
	}
}

func TestWithNamedSessionsRejectsInvalidOrCapacityBypassingConfiguration(t *testing.T) {
	base := Baseline(Limits{LiveSessions: 8, SessionOutputBytes: 1024})
	invalid := []Catalog{
		base.WithNamedSessions(0, 1024, 32),
		base.WithNamedSessions(9, 1024, 32),
		base.WithNamedSessions(8, 0, 32),
		base.WithNamedSessions(8, 2048, 32),
		base.WithNamedSessions(8, 1024, 0),
	}
	for i, got := range invalid {
		if got.Features[FeatureNamedSessions] != Unavailable {
			t.Fatalf("invalid config %d advertised named sessions", i)
		}
	}
}
