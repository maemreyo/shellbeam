package operation

import (
	"testing"

	delegatedsession "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

func TestEmptySessionModePreservesLegacyFingerprintGoldens(t *testing.T) {
	cases := []struct {
		name          string
		intent        Intent
		executable    string
		wantRequest   string
		wantExecution string
	}{
		{"direct", Intent{Command: "printf hi", CWD: "/tmp", TimeoutMS: 10}, "/bin/sh", "4a57658e746aada961e737c4eec0b3443d477c37e0590c15d9839b435d65264f", "9a23fdc9dfa1d06ed51d40b4b5e97b95207e6916104e9bf633c191f8a04ac952"},
		{"direct_tty", Intent{Command: "printf hi", CWD: "/tmp", TTY: true, TimeoutMS: 10}, "/bin/sh", "6d920130b300c6bf7487d8734fcb7094390e29f1571be28a4ea79d62d86a7ff9", "cf903dfb875d0c2fd1c409ce69e70aad11801d426dc6d028e437ce9f65d2f3e4"},
		{"persistent", Intent{Command: "printf hi", CWD: "/tmp", TimeoutMS: 10, Persistent: true, SessionName: "dev-server"}, "/bin/sh", "82cabae7c843f25f99840e564ff2978cc09ff4fb5f7d0d8e77012d68f282efc1", "78f54a5e015efc0fa970ea9dace73d3de6219889cc5d59882a339bae2e269909"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := tc.intent.RequestFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			exec, err := tc.intent.ExecutionFingerprint(tc.executable)
			if err != nil {
				t.Fatal(err)
			}
			if req != tc.wantRequest || exec != tc.wantExecution {
				t.Fatalf("legacy fingerprint changed request=%s execution=%s", req, exec)
			}
		})
	}
}

func TestDelegatedIntentValidationAndFingerprints(t *testing.T) {
	base := Intent{
		Command: "printf hi", CWD: "/tmp", TimeoutMS: 10,
		SessionMode: delegatedsession.ModeDelegatedInteractive,
		SessionName: "agent-shell",
	}
	request, err := base.RequestFingerprint()
	if err != nil {
		t.Fatalf("valid delegated request rejected: %v", err)
	}
	execution, err := base.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatalf("valid delegated execution rejected: %v", err)
	}
	if request == "4a57658e746aada961e737c4eec0b3443d477c37e0590c15d9839b435d65264f" || execution == "9a23fdc9dfa1d06ed51d40b4b5e97b95207e6916104e9bf633c191f8a04ac952" {
		t.Fatal("delegated mode shared legacy fingerprint")
	}
	changed := base
	changed.SessionName = "agent-shell-2"
	changedRequest, err := changed.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if changedRequest == request {
		t.Fatal("delegated session name not bound")
	}

	for name, mutate := range map[string]func(*Intent){
		"tty":          func(v *Intent) { v.TTY = true },
		"persistent":   func(v *Intent) { v.Persistent = true },
		"unknown_mode": func(v *Intent) { v.SessionMode = "future_mode" },
	} {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if _, err := got.RequestFingerprint(); err == nil {
				t.Fatalf("invalid delegated intent accepted: %#v", got)
			}
		})
	}
}

func TestTypedDelegatedIntentBindsModeAndNameWithoutChangingLegacyTypedFingerprint(t *testing.T) {
	legacy := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	legacyFP, err := legacy.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if legacyFP != "8ab3a04868e469d1965932338e39ea2b5a0f16e8ffc4f5e006a880c463bab102" {
		t.Fatalf("legacy typed fingerprint changed: %s", legacyFP)
	}
	delegated := legacy
	delegated.SessionMode = delegatedsession.ModeDelegatedInteractive
	delegated.SessionName = "typed-shell"
	first, err := delegated.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == legacyFP {
		t.Fatal("typed delegated mode not bound")
	}
	changed := delegated
	changed.SessionName = "typed-shell-2"
	second, err := changed.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("typed delegated session name not bound")
	}
	badTTY := delegated
	badTTY.TTY = true
	if _, err := badTTY.Fingerprint(); err == nil {
		t.Fatal("typed delegated tty accepted")
	}
	badPersistent := delegated
	badPersistent.Persistent = true
	if _, err := badPersistent.Fingerprint(); err == nil {
		t.Fatal("typed delegated persistent accepted")
	}
	badMode := legacy
	badMode.SessionMode = "future_mode"
	if _, err := badMode.Fingerprint(); err == nil {
		t.Fatal("unknown typed session mode accepted")
	}
}

func TestDelegatedReservationIsNeverOrdinaryEvidenceEligible(t *testing.T) {
	base := Reservation{SchemaVersion: 5, SessionMode: delegatedsession.ModeDelegatedInteractive, AuthorityEpoch: 1}
	cases := []Reservation{
		func() Reservation {
			v := base
			v.Evidence = &evidence.Contract{VerificationKind: evidence.VerificationTest}
			return v
		}(),
		func() Reservation { v := base; v.Intent = &DeclaredIntent{Kind: IntentKindTest}; return v }(),
		func() Reservation {
			v := base
			v.ProjectCommand = &project.CommandBinding{SchemaVersion: project.BindingSchemaVersion, Kind: "build"}
			return v
		}(),
	}
	for i, reservation := range cases {
		if reservation.EvidenceEligible() {
			t.Fatalf("delegated reservation[%d] promoted to ordinary evidence", i)
		}
	}
}

func TestDelegatedFingerprintStillBindsResourceAndTracePolicy(t *testing.T) {
	base := Intent{Command: "true", CWD: "/tmp", SessionMode: delegatedsession.ModeDelegatedInteractive}
	plain, err := base.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	limited := base
	limited.ResourceLimits = &ResourceLimits{MemoryBytes: 1024}
	limitedFP, err := limited.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if limitedFP == plain {
		t.Fatal("delegated resource policy not bound")
	}

	traced := base
	traced.TraceMode = "best_effort"
	tracedFP, err := traced.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if tracedFP == plain {
		t.Fatal("delegated trace policy not bound")
	}
}

func TestTypedDelegatedPolicyFieldsBindFingerprintWithoutChangingUnsetLegacyTypedFingerprint(t *testing.T) {
	legacy := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	legacyFP, err := legacy.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if legacyFP != "8ab3a04868e469d1965932338e39ea2b5a0f16e8ffc4f5e006a880c463bab102" {
		t.Fatalf("legacy fingerprint changed: %s", legacyFP)
	}
	base := legacy
	base.SessionMode = delegatedsession.ModeDelegatedInteractive
	plain, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	stream := base
	stream.StdinMode = StdinModeStream
	streamFP, err := stream.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if streamFP == plain {
		t.Fatal("typed delegated stdin policy not bound")
	}
	unlimited := base
	unlimited.TimeoutMode = TimeoutModeUnlimited
	unlimitedFP, err := unlimited.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if unlimitedFP == plain {
		t.Fatal("typed delegated timeout policy not bound")
	}
	bad := base
	bad.StdinMode = StdinMode("future")
	if _, err := bad.Fingerprint(); err == nil {
		t.Fatal("invalid typed stdin policy accepted")
	}
	bad = base
	bad.TimeoutMode = TimeoutMode("future")
	if _, err := bad.Fingerprint(); err == nil {
		t.Fatal("invalid typed timeout policy accepted")
	}
}
