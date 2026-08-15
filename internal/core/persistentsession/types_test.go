package persistentsession

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSessionName(t *testing.T) {
	good := []string{"dev-server", "Build_01", "alpha server", "日本語"}
	for _, name := range good {
		if err := ValidateSessionName(name); err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	}
	bad := []string{"", " dev", "dev ", "dev/server", `dev\\server`, "dev\nserver", "dev\x00server", strings.Repeat("x", MaxSessionNameBytes+1)}
	for _, name := range bad {
		if err := ValidateSessionName(name); err == nil {
			t.Fatalf("invalid name %q accepted", name)
		}
	}
}

func TestBindingValidationRequiresClosedPersistentIdentity(t *testing.T) {
	now := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	base := Binding{
		SchemaVersion:          SchemaVersion,
		SessionID:              "01M00000000000000000000000",
		OperationID:            "op-persistent",
		SessionName:            "dev-server",
		Persistent:             true,
		Supervision:            SupervisionPerSession,
		Continuity:             ContinuityDaemonRestart,
		SupervisorGenerationID: "gen_01M00000000000000000000000",
		SupervisorEndpointRef:  "supref_01M00000000000000000000000",
		Lifecycle:              LifecycleProvisioning,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	cases := map[string]func(*Binding){
		"schema":      func(v *Binding) { v.SchemaVersion++ },
		"persistent":  func(v *Binding) { v.Persistent = false },
		"supervision": func(v *Binding) { v.Supervision = "shared" },
		"continuity":  func(v *Binding) { v.Continuity = "host_reboot" },
		"generation":  func(v *Binding) { v.SupervisorGenerationID = "" },
		"endpoint":    func(v *Binding) { v.SupervisorEndpointRef = "/tmp/control.sock" },
		"lifecycle":   func(v *Binding) { v.Lifecycle = "unknown" },
		"created":     func(v *Binding) { v.CreatedAt = time.Time{} },
		"updated":     func(v *Binding) { v.UpdatedAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid binding accepted: %#v", got)
			}
		})
	}
}

func TestPersistentLimitsAreClosed(t *testing.T) {
	if MaxSessionNameBytes != 128 || DefaultInspectRows != 25 || MaxInspectRows != 100 {
		t.Fatalf("inspect/name limits changed: %d %d %d", MaxSessionNameBytes, DefaultInspectRows, MaxInspectRows)
	}
	if MaxInputRecords != 4096 || MaxInputRecordMetadataBytes != 1<<20 || MaxKillRecords != 256 {
		t.Fatalf("ledger limits changed: %d %d %d", MaxInputRecords, MaxInputRecordMetadataBytes, MaxKillRecords)
	}
	if ReattachHandshakeTimeoutMS != 2000 || StartupReattachConcurrency != 16 || StartupReattachBudgetMS != 5000 {
		t.Fatalf("reattach limits changed: %d %d %d", ReattachHandshakeTimeoutMS, StartupReattachConcurrency, StartupReattachBudgetMS)
	}
}
