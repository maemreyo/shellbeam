package operation

import (
	"strings"
	"testing"
	"time"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func TestContextExecBindingBindsParentAuthorityAndActualExecutionIdentity(t *testing.T) {
	binding := ContextExecBinding{
		ContextExecID:      "ctxexec_binding_01",
		ParentSessionID:    "parent_session_01",
		AuthorityEpoch:     delegated.AuthorityEpoch(4),
		RequestFingerprint: strings.Repeat("a", 64),
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("binding: %v", err)
	}
	digest, err := binding.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	fingerprint, err := binding.ExecutionFingerprint("/tmp/project", "/usr/bin/go")
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("execution fingerprint=%q err=%v", fingerprint, err)
	}

	cases := map[string]func(*ContextExecBinding){
		"context_exec_id": func(v *ContextExecBinding) { v.ContextExecID += "x" },
		"parent_session":  func(v *ContextExecBinding) { v.ParentSessionID += "x" },
		"epoch":           func(v *ContextExecBinding) { v.AuthorityEpoch++ },
		"request":         func(v *ContextExecBinding) { v.RequestFingerprint = strings.Repeat("b", 64) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := binding
			mutate(&changed)
			got, err := changed.ExecutionFingerprint("/tmp/project", "/usr/bin/go")
			if err != nil {
				t.Fatal(err)
			}
			if got == fingerprint {
				t.Fatalf("%s did not change execution fingerprint", name)
			}
		})
	}
	if got, _ := binding.ExecutionFingerprint("/tmp/other", "/usr/bin/go"); got == fingerprint {
		t.Fatal("cwd did not bind execution fingerprint")
	}
	if got, _ := binding.ExecutionFingerprint("/tmp/project", "/opt/go/bin/go"); got == fingerprint {
		t.Fatal("actual executable did not bind execution fingerprint")
	}
}

func TestContextExecStateKeepsExactRequestAndExpectationImmutable(t *testing.T) {
	req := contextexec.Request{ContextExecID: "ctxexec_state_01", SessionID: "parent_session_01", AuthorityEpoch: 4, Argv: []string{"go", "test", "./..."}, TimeoutMS: 30_000, MaxOutputBytes: 1 << 20}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	state := ContextExecState{
		SchemaVersion:      ContextExecStateSchemaVersion,
		Request:            req,
		RequestFingerprint: fp,
		Expectation:        contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_state_01", ShellIdentity: "fish:runtime_01", CWDObserved: "/tmp/project", PrivacyState: "standard"},
		Lifecycle:          contextexec.LifecycleReserved,
		CreatedAt:          time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("state: %v", err)
	}
	clone := state.Clone()
	clone.Request.Argv[0] = "changed"
	if state.Request.Argv[0] != "go" {
		t.Fatal("state clone shared request argv")
	}

	bad := state
	bad.Expectation.SessionID = "other_parent"
	if err := bad.Validate(); err == nil {
		t.Fatal("context state accepted mismatched parent session")
	}
	bad = state
	bad.Expectation.AuthorityEpoch++
	if err := bad.Validate(); err == nil {
		t.Fatal("context state accepted mismatched epoch")
	}
	bad = state
	bad.RequestFingerprint = strings.Repeat("b", 64)
	if err := bad.Validate(); err == nil {
		t.Fatal("context state accepted mismatched request fingerprint")
	}
}

func TestContextExecV6ReservationCarriesExplicitParentBindingWithoutChangingLegacyIntentFingerprints(t *testing.T) {
	legacy := Intent{Argv: []string{"echo", "ok"}, CWD: "/tmp", TimeoutMS: 1000}
	before, err := legacy.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	binding := &ContextExecBinding{ContextExecID: "ctxexec_v6_01", ParentSessionID: "parent_session_01", AuthorityEpoch: 4, RequestFingerprint: strings.Repeat("a", 64)}
	execFP, err := binding.ExecutionFingerprint("/tmp/project", "/usr/bin/printf")
	if err != nil {
		t.Fatal(err)
	}
	reservation := Reservation{
		SchemaVersion: ContextExecReservationSchemaVersion,
		OperationID:   "context_child_op_01", SessionID: "context_child_session_01",
		RequestFingerprint: binding.RequestFingerprint, ExecutionFingerprint: execFP,
		ExecutionMode: ExecutionModeArgv, Executable: "/usr/bin/printf", Argv: []string{"printf", "ok"}, CWD: "/tmp/project", TimeoutMS: 1000,
		DaemonIncarnation: "daemon", ContextExec: binding,
	}
	if reservation.ContextExec == nil || reservation.ContextExec.ContextExecID != binding.ContextExecID {
		t.Fatalf("reservation=%#v", reservation)
	}
	after, err := legacy.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("legacy request fingerprint changed: %q -> %q", before, after)
	}
}
