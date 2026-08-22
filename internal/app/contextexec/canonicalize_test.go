package contextexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestRecordTerminalPromotesOnlyDaemonValidatedChildTruthThenReleasesLease(t *testing.T) {
	svc, store, spawned := canonicalSpawnedFixture(t)
	terminal := validChildTerminalResult(t, spawned)
	events := []string{}
	store.events = &events
	got, err := svc.RecordTerminal(context.Background(), spawned, TerminalTruth{Result: terminal, StdoutBytes: 0, StderrBytes: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleCanonicalized || got.Result == nil || got.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 {
		t.Fatalf("canonical=%#v", got)
	}
	if strings.Join(events, ",") != "reserve_operation,advance_child_terminal,advance_canonicalized,publish_receipt,release_lease" {
		t.Fatalf("events=%q", strings.Join(events, ","))
	}
	if store.releaseCalls != 1 {
		t.Fatalf("release=%d", store.releaseCalls)
	}
}

func TestRecordTerminalRejectsHelperAuthorityByteOrExecutableDriftBeforePromotion(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TerminalTruth)
	}{
		{"helper authority", func(v *TerminalTruth) { v.Result.EvidenceAuthority = core.EvidenceAuthorityContextExecChildOwnedV1 }},
		{"byte count", func(v *TerminalTruth) { v.StdoutBytes = 1 }},
		{"executable", func(v *TerminalTruth) { v.Result.Executable.ResolvedPath = "/usr/bin/other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, spawned := canonicalSpawnedFixture(t)
			truth := TerminalTruth{Result: validChildTerminalResult(t, spawned)}
			tc.mutate(&truth)
			store.advanceCalls = 0
			if _, err := svc.RecordTerminal(context.Background(), spawned, truth); err == nil {
				t.Fatal("terminal drift accepted")
			}
			if store.advanceCalls != 0 || store.releaseCalls != 0 {
				t.Fatalf("advance=%d release=%d", store.advanceCalls, store.releaseCalls)
			}
		})
	}
}

func TestCanonicalizeNoChildFailureHandlesPrepareAndFailedSpawnWithoutMechanicalAuthority(t *testing.T) {
	req := admissionRequest()
	for _, attempted := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare", true: "spawn"}[attempted], func(t *testing.T) {
			state := helperAuthenticatedState(t, req)
			store := &admissionStoreFake{state: state, found: true}
			svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})
			truth := NoChildFailureTruth{FailureCode: string(failure.ContextExecUnavailable)}
			if attempted {
				authorized, auth, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
				if err != nil {
					t.Fatal(err)
				}
				state = authorized
				truth.ResolvedExecutable = auth.ResolvedExecutable
				truth.Spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: string(failure.ContextExecUnavailable)}
			}
			events := []string{}
			store.events = &events
			got, err := svc.CanonicalizeNoChildFailure(context.Background(), state, truth)
			if err != nil {
				t.Fatal(err)
			}
			if got.Lifecycle != core.LifecycleCanonicalized || got.Result == nil || got.Result.EvidenceAuthority != "" || got.Result.EvidenceQuality != core.EvidenceQualityUnproven {
				t.Fatalf("canonical failure=%#v", got)
			}
			wantEvents := "advance_canonicalized,release_lease"
			if attempted {
				wantEvents = "reserve_operation," + wantEvents
			}
			if strings.Join(events, ",") != wantEvents {
				t.Fatalf("events=%q want=%q", strings.Join(events, ","), wantEvents)
			}
		})
	}
}

func canonicalSpawnedFixture(t *testing.T) (*Service, *admissionStoreFake, operation.ContextExecState) {
	t.Helper()
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	store := &admissionStoreFake{state: state, found: true}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})
	authorized, auth, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := svc.RecordSpawn(context.Background(), authorized, SpawnTruth{ChildOperationID: auth.ChildOperationID, ChildSessionID: auth.ChildSessionID, ResolvedExecutable: auth.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, spawned
}

var _ = errors.Is
