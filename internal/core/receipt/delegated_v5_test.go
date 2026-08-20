package receipt

import (
	"reflect"
	"strings"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDelegatedV5ReceiptValidation(t *testing.T) {
	zero := 0
	base := Receipt{
		SchemaVersion: 5, OperationID: "op-v5", SessionID: "session-v5",
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		DaemonIncarnation: "daemon", ExecutionMode: "shell", Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/tmp",
		State: session.Completed, Outcome: session.Success, OutputComplete: true,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: false, Code: &zero},
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1,
		EvidenceAuthority: EvidenceAuthoritySessionLifecycleOnly, InputAuthorityProvenance: InputAuthorityAgentOnly,
		CaptureQuality: CaptureComplete,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid v5 rejected: %v", err)
	}

	incompleteSuccess := base
	incompleteSuccess.OutputComplete = false
	incompleteSuccess.CaptureQuality = CaptureIncomplete
	incompleteSuccess.CaptureReasons = []CaptureReason{CaptureReasonTransportGap}
	if err := incompleteSuccess.Validate(); err != nil {
		t.Fatalf("v5 lifecycle success with truthful incomplete capture rejected: %v", err)
	}
	params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fp, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	typed := base
	typed.ProjectCommand = &project.CommandBinding{SchemaVersion: project.BindingSchemaVersion, ManifestDigest: strings.Repeat("c", 64), ManifestSchemaVersion: project.ManifestSchemaV2, CommandID: "test", ParameterFingerprint: fp, Parameters: params, ResolvedArgv: []string{"go", "test", "./internal/app"}, LogicalCWD: ".", ResolvedCWD: "/tmp"}
	if err := typed.Validate(); err != nil {
		t.Fatalf("typed v5 rejected: %v", err)
	}

	cases := map[string]func(*Receipt){
		"mode":               func(v *Receipt) { v.SessionMode = "future" },
		"epoch":              func(v *Receipt) { v.AuthorityEpoch = 0 },
		"request":            func(v *Receipt) { v.RequestFingerprint = "" },
		"execution":          func(v *Receipt) { v.ExecutionFingerprint = "" },
		"persistent":         func(v *Receipt) { v.Persistent = true },
		"tty":                func(v *Receipt) { v.TTY = true },
		"evidence_authority": func(v *Receipt) { v.EvidenceAuthority = "ordinary" },
		"provenance":         func(v *Receipt) { v.InputAuthorityProvenance = "future" },
		"capture": func(v *Receipt) {
			v.CaptureQuality = CaptureIncomplete
			v.CaptureReasons = nil
			v.OutputComplete = false
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid v5 accepted: %#v", got)
			}
		})
	}
}

func TestReceiptSchemasOneThroughFourRejectDelegatedFields(t *testing.T) {
	for schema := 1; schema <= 4; schema++ {
		r := Receipt{SchemaVersion: schema, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, CaptureQuality: CaptureComplete, EvidenceAuthority: EvidenceAuthoritySessionLifecycleOnly, InputAuthorityProvenance: InputAuthorityAgentOnly}
		if err := r.Validate(); err == nil {
			t.Fatalf("schema %d accepted delegated fields", schema)
		}
	}
}

func TestDelegatedV5PrivateCaptureTruthIsPartialAndComposable(t *testing.T) {
	zero := 0
	base := Receipt{
		SchemaVersion: 5, OperationID: "op-v5-private", SessionID: "session-v5-private",
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		DaemonIncarnation: "daemon", State: session.Completed, Outcome: session.Success,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Code: &zero},
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 2,
		EvidenceAuthority: EvidenceAuthoritySessionLifecycleOnly, InputAuthorityProvenance: InputAuthorityHumanWriteGranted,
	}
	truth := CompleteCaptureTruth()
	var err error
	truth, err = truth.WithReason(CaptureReasonPrivateIntervalsOmitted)
	if err != nil {
		t.Fatal(err)
	}
	private := base
	private.OutputComplete, private.CaptureQuality, private.CaptureReasons = truth.OutputComplete, truth.Quality, truth.Reasons
	if err := private.Validate(); err != nil {
		t.Fatalf("privacy-only v5 rejected: %v", err)
	}
	if private.OutputComplete || private.CaptureQuality != CapturePartial || len(private.CaptureReasons) != 1 || private.CaptureReasons[0] != CaptureReasonPrivateIntervalsOmitted {
		t.Fatalf("private capture=%#v", private)
	}
	truth, err = truth.WithReason(CaptureReasonTransportGap)
	if err != nil {
		t.Fatal(err)
	}
	truth, err = truth.WithReason(CaptureReasonProviderLost)
	if err != nil {
		t.Fatal(err)
	}
	combined := base
	combined.OutputComplete, combined.CaptureQuality, combined.CaptureReasons = truth.OutputComplete, truth.Quality, truth.Reasons
	if err := combined.Validate(); err != nil {
		t.Fatalf("composed v5 rejected: %v", err)
	}
	want := []CaptureReason{CaptureReasonPrivateIntervalsOmitted, CaptureReasonTransportGap, CaptureReasonProviderLost}
	if combined.OutputComplete || combined.CaptureQuality != CaptureIncomplete || !reflect.DeepEqual(combined.CaptureReasons, want) {
		t.Fatalf("combined capture=%#v want=%v", combined.CaptureReasons, want)
	}
}
