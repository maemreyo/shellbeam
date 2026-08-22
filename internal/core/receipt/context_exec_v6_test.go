package receipt

import (
	"strings"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestContextExecV6ReceiptRequiresExactMechanicalAuthorityAndExecutableProvenance(t *testing.T) {
	code := 0
	rec := Receipt{
		SchemaVersion:        6,
		OperationID:          "cxop_" + strings.Repeat("a", 64),
		SessionID:            "cxs_" + strings.Repeat("b", 64),
		RequestFingerprint:   strings.Repeat("c", 64),
		ExecutionFingerprint: strings.Repeat("d", 64),
		DaemonIncarnation:    "daemon_ctx",
		ExecutionMode:        "argv",
		Executable:           "/usr/bin/go",
		State:                session.Completed,
		Outcome:              session.Success,
		CWD:                  "/tmp/project",
		TimeoutMS:            1000,
		AuthorityEpoch:       delegated.AuthorityEpoch(4),
		EvidenceAuthority:    EvidenceAuthorityContextExecChildOwnedV1,
		OutputBytes:          3,
		OutputComplete:       true,
		StdinClosed:          true,
		StdinMode:            "closed",
		TimeoutSource:        "requested",
		StdinModeSource:      "default",
		Spawn:                SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:                 ExitEvidence{Reaped: true, Code: &code},
		ContextExec: &ContextExecProvenance{
			ContextExecID:       "ctxexec_v6",
			ParentSessionID:     "parent_session_v6",
			AuthorityEpoch:      delegated.AuthorityEpoch(4),
			RequestedExecutable: "go",
			ResolvedExecutable:  "/usr/bin/go",
		},
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("valid v6: %v", err)
	}
	cases := map[string]func(*Receipt){
		"authority":         func(v *Receipt) { v.EvidenceAuthority = "" },
		"context":           func(v *Receipt) { v.ContextExec = nil },
		"epoch":             func(v *Receipt) { v.ContextExec.AuthorityEpoch++ },
		"requested":         func(v *Receipt) { v.ContextExec.RequestedExecutable = "" },
		"resolved mismatch": func(v *Receipt) { v.ContextExec.ResolvedExecutable = "/usr/bin/other" },
		"spawn":             func(v *Receipt) { v.Spawn.Succeeded = false },
		"reap":              func(v *Receipt) { v.Exit.Reaped = false },
		"delegated mode":    func(v *Receipt) { v.SessionMode = delegated.ModeDelegatedInteractive },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := rec
			ctx := *rec.ContextExec
			bad.ContextExec = &ctx
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatalf("invalid v6 accepted: %#v", bad)
			}
		})
	}
}

func TestContextExecV6ResultProjectsOnlyScopedProvenance(t *testing.T) {
	rec := validContextExecV6Receipt(t)
	got, err := NewResult(ResultInput{
		OperationID: rec.OperationID, SessionID: rec.SessionID,
		State: rec.State, Outcome: rec.Outcome,
		RawBytes: rec.OutputBytes, NextCursor: rec.OutputBytes,
		Receipt: &rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.EvidenceAuthority != EvidenceAuthorityContextExecChildOwnedV1 || got.ContextExec == nil || *got.ContextExec != *rec.ContextExec {
		t.Fatalf("projection=%#v", got)
	}
	if got.SessionMode != "" || got.InputAuthorityProvenance != "" {
		t.Fatalf("context exec projected delegated authority: %#v", got)
	}
}

func validContextExecV6Receipt(t *testing.T) Receipt {
	t.Helper()
	code := 0
	return Receipt{
		SchemaVersion: 6, OperationID: "cxop_" + strings.Repeat("a", 64), SessionID: "cxs_" + strings.Repeat("b", 64),
		RequestFingerprint: strings.Repeat("c", 64), ExecutionFingerprint: strings.Repeat("d", 64), DaemonIncarnation: "daemon_ctx",
		ExecutionMode: "argv", Executable: "/usr/bin/go", State: session.Completed, Outcome: session.Success, CWD: "/tmp/project",
		TimeoutMS: 1000, AuthorityEpoch: 4, EvidenceAuthority: EvidenceAuthorityContextExecChildOwnedV1,
		OutputBytes: 3, OutputComplete: true, StdinClosed: true, StdinMode: "closed", TimeoutSource: "requested", StdinModeSource: "default",
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: true, Code: &code},
		ContextExec: &ContextExecProvenance{ContextExecID: "ctxexec_v6", ParentSessionID: "parent_session_v6", AuthorityEpoch: 4, RequestedExecutable: "go", ResolvedExecutable: "/usr/bin/go"},
	}
}
