package receipt

import (
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
