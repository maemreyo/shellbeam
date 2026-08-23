package contextexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type publicationStoreFake struct {
	*admissionStoreFake
	output     []byte
	published  receipt.Receipt
	publishErr error
}

func (f *publicationStoreFake) ReadOutput(_ context.Context, _ operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	if cursor < 0 || cursor > int64(len(f.output)) {
		return nil, 0, errors.New("cursor_out_of_range")
	}
	end := len(f.output)
	if max >= 0 && int(cursor)+max < end {
		end = int(cursor) + max
	}
	out := append([]byte(nil), f.output[cursor:end]...)
	return out, int64(end), nil
}

func (f *publicationStoreFake) AppendOutput(_ context.Context, _ operation.SessionID, data []byte) (int, MutationResult) {
	if f.events != nil {
		*f.events = append(*f.events, "persist_output")
	}
	f.output = append(f.output, data...)
	return len(data), MutationResult{Durability: DurableChange}
}

func (f *publicationStoreFake) PublishTerminal(_ context.Context, rec receipt.Receipt) MutationResult {
	if f.events != nil {
		*f.events = append(*f.events, "publish_receipt")
	}
	if f.publishErr != nil {
		return MutationResult{Durability: NoDurableChange, Err: f.publishErr}
	}
	f.published = rec
	return MutationResult{Durability: DurableChange}
}

type terminalSchedulerFake struct {
	events      *[]string
	calls       int
	rec         receipt.Receipt
	reservation operation.Reservation
}

func (f *terminalSchedulerFake) ScheduleContextTerminal(_ context.Context, rec receipt.Receipt, reservation operation.Reservation) error {
	f.calls++
	f.rec, f.reservation = rec, reservation
	if f.events != nil {
		*f.events = append(*f.events, "schedule_terminal")
	}
	return nil
}

func TestRecordTerminalPublishesCanonicalChildBeforeWorkersAndLeaseRelease(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	base := &admissionStoreFake{state: state, found: true}
	store := &publicationStoreFake{admissionStoreFake: base}
	scheduler := &terminalSchedulerFake{}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6", TerminalScheduler: scheduler})
	authorized, auth, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := svc.RecordSpawn(context.Background(), authorized, SpawnTruth{ChildOperationID: auth.ChildOperationID, ChildSessionID: auth.ChildSessionID, ResolvedExecutable: auth.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}})
	if err != nil {
		t.Fatal(err)
	}
	terminal := validChildTerminalResult(t, spawned)
	terminal.Output.StdoutBytes = 2
	terminal.Output.StderrBytes = 1
	events := []string{}
	base.events = &events
	scheduler.events = &events
	got, err := svc.RecordTerminal(context.Background(), spawned, TerminalTruth{Result: terminal, StdoutBytes: 2, StderrBytes: 1, CombinedOutput: []byte("abc")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleCanonicalized || got.Result == nil || got.Result.EvidenceAuthority != core.EvidenceAuthorityContextExecChildOwnedV1 {
		t.Fatalf("canonical=%#v", got)
	}
	want := "reserve_operation,persist_output,advance_child_terminal,advance_canonicalized,publish_receipt,schedule_terminal,release_lease"
	if gotEvents := strings.Join(events, ","); gotEvents != want {
		t.Fatalf("events=%q want=%q", gotEvents, want)
	}
	if scheduler.calls != 1 || store.published.SchemaVersion != 6 || store.published.EvidenceAuthority != receipt.EvidenceAuthorityContextExecChildOwnedV1 {
		t.Fatalf("scheduler=%d receipt=%#v", scheduler.calls, store.published)
	}
	if store.published.OperationID != string(spawned.ChildOperationID) || store.published.SessionID != string(spawned.ChildSessionID) || store.published.OutputBytes != 3 || store.published.ContextExec == nil || store.published.ContextExec.RequestedExecutable != req.Argv[0] || store.published.ContextExec.ResolvedExecutable != "/usr/bin/go" {
		t.Fatalf("published receipt=%#v", store.published)
	}
}

func TestRecordTerminalReceiptPublishFailureRetainsLeaseAndDoesNotSchedule(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	base := &admissionStoreFake{state: state, found: true}
	store := &publicationStoreFake{admissionStoreFake: base, publishErr: errors.New("disk unavailable")}
	scheduler := &terminalSchedulerFake{}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6", TerminalScheduler: scheduler})
	authorized, auth, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := svc.RecordSpawn(context.Background(), authorized, SpawnTruth{ChildOperationID: auth.ChildOperationID, ChildSessionID: auth.ChildSessionID, ResolvedExecutable: auth.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}})
	if err != nil {
		t.Fatal(err)
	}
	terminal := validChildTerminalResult(t, spawned)
	terminal.Output.StdoutBytes = 1
	events := []string{}
	base.events = &events
	scheduler.events = &events
	got, err := svc.RecordTerminal(context.Background(), spawned, TerminalTruth{Result: terminal, StdoutBytes: 1, CombinedOutput: []byte("x")})
	if err == nil {
		t.Fatal("receipt publication failure accepted")
	}
	if got.Lifecycle != core.LifecycleCanonicalized {
		t.Fatalf("durable state=%#v", got)
	}
	if base.releaseCalls != 0 || scheduler.calls != 0 {
		t.Fatalf("release=%d scheduler=%d", base.releaseCalls, scheduler.calls)
	}
	if gotEvents := strings.Join(events, ","); gotEvents != "reserve_operation,persist_output,advance_child_terminal,advance_canonicalized,publish_receipt" {
		t.Fatalf("events=%q", gotEvents)
	}
}
