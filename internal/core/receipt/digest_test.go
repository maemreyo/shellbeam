package receipt

import (
	"encoding/json"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDigestIsStableAndBindsTerminalReceipt(t *testing.T) {
	rec := digestTestReceipt()
	first, err := Digest(rec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Digest(rec)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("digest first=%q second=%q err=%v", first, second, err)
	}
	changed := rec
	changed.OutputBytes++
	other, err := Digest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("receipt mutation did not change digest")
	}
}

func TestDigestRejectsNonTerminalOrInvalidReceipt(t *testing.T) {
	rec := digestTestReceipt()
	rec.State = session.Running
	rec.Outcome = session.NoOutcome
	rec.Exit = ExitEvidence{}
	if _, err := Digest(rec); err == nil {
		t.Fatal("non-terminal receipt accepted")
	}
	invalid := digestTestReceipt()
	invalid.RequestFingerprint = ""
	if _, err := Digest(invalid); err == nil {
		t.Fatal("invalid receipt accepted")
	}
}

func digestTestReceipt() Receipt {
	code := 0
	return Receipt{
		SchemaVersion: 2, OperationID: "op-telemetry", SessionID: "session-telemetry",
		RequestFingerprint: "request", ExecutionFingerprint: "execution", ObservationBindingFingerprint: "observation",
		DaemonIncarnation: "daemon", ExecutionMode: "argv", Executable: "/bin/true",
		State: session.Completed, Outcome: session.Success, OutputBytes: 12, OutputComplete: true,
		InputAcceptedBytes: 4, InputDeliveredBytes: 4, Spawn: SpawnEvidence{Attempted: true, Succeeded: true},
		Exit: ExitEvidence{Reaped: true, Code: &code},
	}
}

func TestLocalResourceEvidenceDoesNotChangeReceiptJSONOrDigest(t *testing.T) {
	base := digestTestReceipt()
	baseJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, err := Digest(base)
	if err != nil {
		t.Fatal(err)
	}
	value := int64(17)
	withResources := base
	withResources.Exit.Resources = &ResourceEvidence{
		CPUUserMS:        ResourceMetric{Quality: ResourcePlatformReported, Value: &value},
		CPUSystemMS:      ResourceMetric{Quality: ResourcePlatformReported, Value: &value},
		MaxRSSBytes:      ResourceMetric{Quality: ResourcePlatformReported, Value: &value},
		ReadBytes:        ResourceMetric{Quality: ResourceUnavailable},
		WriteBytes:       ResourceMetric{Quality: ResourceUnavailable},
		ProcessCountPeak: ResourceMetric{Quality: ResourceSampled, Value: &value},
	}
	resourceJSON, err := json.Marshal(withResources)
	if err != nil {
		t.Fatal(err)
	}
	resourceDigest, err := Digest(withResources)
	if err != nil {
		t.Fatal(err)
	}
	if string(resourceJSON) != string(baseJSON) {
		t.Fatalf("local resource evidence changed public receipt JSON\nbase=%s\nwith=%s", baseJSON, resourceJSON)
	}
	if resourceDigest != baseDigest {
		t.Fatalf("local resource evidence changed receipt digest %s -> %s", baseDigest, resourceDigest)
	}
}
