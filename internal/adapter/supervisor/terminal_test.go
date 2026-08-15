package supervisor

import (
	"os"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestTerminalRecordSealVerifyAndPrivateRoundTrip(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(t.TempDir()+"/runtime", "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	record := TerminalRecord{
		SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion,
		SessionID: "persistent-session-a", GenerationID: "generation-a",
		State: session.Completed, Outcome: session.Success,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
		OutputBytes: 12, OutputComplete: true, InputAcceptedBytes: 3, InputDeliveredBytes: 3, StdinClosed: true,
	}
	sealed, err := SealTerminalRecord(capability, record)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Integrity == "" || VerifyTerminalRecord(capability, sealed, record.SessionID, record.GenerationID) != nil {
		t.Fatalf("sealed record invalid: %#v", sealed)
	}
	if err := WriteTerminalRecord(layout, sealed); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTerminalRecord(layout, capability, record.SessionID, record.GenerationID)
	if err != nil || loaded.Integrity != sealed.Integrity {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	info, err := os.Lstat(layout.TerminalPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("terminal permissions=%v err=%v", info, err)
	}
}

func TestTerminalRecordRejectsWrongIdentityCapabilityTamperAndMalformedFile(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(t.TempDir()+"/runtime", "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	record, err := SealTerminalRecord(capability, TerminalRecord{
		SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion,
		SessionID: "persistent-session-a", GenerationID: "generation-a", State: session.Failed, Outcome: session.Failure,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true}, OutputComplete: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, verify := range map[string]func() error{
		"wrong_session":    func() error { return VerifyTerminalRecord(capability, record, "persistent-session-b", "generation-a") },
		"wrong_generation": func() error { return VerifyTerminalRecord(capability, record, "persistent-session-a", "generation-b") },
		"wrong_capability": func() error { return VerifyTerminalRecord(other, record, "persistent-session-a", "generation-a") },
		"tampered": func() error {
			changed := record
			changed.OutputBytes++
			return VerifyTerminalRecord(capability, changed, "persistent-session-a", "generation-a")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verify(); err == nil {
				t.Fatal("invalid terminal record accepted")
			}
		})
	}
	if err := os.WriteFile(layout.TerminalPath, []byte(`{"schema_version":1,"unknown":"secret-sentinel"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTerminalRecord(layout, capability, "persistent-session-a", "generation-a"); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("malformed terminal error unsafe: %v", err)
	}
}

func TestTerminalRecordRejectsContradictoryOutcomeTimeoutAndGenerationAtWrite(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(t.TempDir()+"/runtime", "persistent-session-a", "generation-a", capability)
	if err != nil {
		t.Fatal(err)
	}
	base := TerminalRecord{SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion, SessionID: "persistent-session-a", GenerationID: "generation-a", State: session.Failed, Outcome: session.Failure, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true}}
	for name, mutate := range map[string]func(*TerminalRecord){
		"outcome":   func(v *TerminalRecord) { v.Outcome = session.Success },
		"timeout":   func(v *TerminalRecord) { v.State = session.TimedOut; v.Outcome = session.Timeout; v.TimedOut = false },
		"abandoned": func(v *TerminalRecord) { v.State = session.Abandoned; v.Outcome = session.Ambiguous },
	} {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if _, err := SealTerminalRecord(capability, got); err == nil {
				t.Fatal("contradictory terminal record accepted")
			}
		})
	}
	wrongGeneration := base
	wrongGeneration.GenerationID = "generation-b"
	sealed, err := SealTerminalRecord(capability, wrongGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTerminalRecord(layout, sealed); err == nil {
		t.Fatal("terminal record for wrong layout generation accepted")
	}
}
