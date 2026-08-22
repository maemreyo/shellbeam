package contextexec

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	receipt "github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type admissionStoreFake struct {
	state                  operation.ContextExecState
	found                  bool
	lookupErr              error
	lease                  operation.ContextExecLease
	leaseFound             bool
	leaseErr               error
	lookups                int
	leaseReads             int
	reserveCalls           int
	advanceCalls           int
	acquireCalls           int
	releaseCalls           int
	bindCalls              int
	operationReserveCalls  int
	reservedOperation      operation.Reservation
	operationReserveResult MutationResult
	output                 []byte
	publishedReceipt       receipt.Receipt
	boundHelper            core.HelperBinding
	boundContext           core.ContextBinding
	boundAt                time.Time
	boundVerifier          string
	bindResult             MutationResult
	reservedWant           operation.ContextExecState
	transitions            []operation.ContextExecTransition
	events                 *[]string
	reserveResult          MutationResult
	advanceResult          MutationResult
	acquireResult          MutationResult
	releaseResult          MutationResult
	recoveryCandidates     []operation.ContextExecState
	recoveryErr            error
	recoveryListCalls      int
}

func (f *admissionStoreFake) LookupContextExec(context.Context, string) (operation.ContextExecState, bool, error) {
	f.lookups++
	if f.events != nil {
		*f.events = append(*f.events, "lookup")
	}
	return f.state.Clone(), f.found, f.lookupErr
}

func (f *admissionStoreFake) FindContextExecLease(context.Context, operation.SessionID, delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error) {
	f.leaseReads++
	if f.events != nil {
		*f.events = append(*f.events, "find_lease")
	}
	return f.lease, f.leaseFound, f.leaseErr
}

func (f *admissionStoreFake) ReserveOperation(_ context.Context, want operation.Reservation) (operation.Reservation, bool, MutationResult) {
	f.operationReserveCalls++
	if f.events != nil {
		*f.events = append(*f.events, "reserve_operation")
	}
	if f.operationReserveResult.Durability == "" {
		f.operationReserveResult.Durability = DurableChange
	}
	if f.operationReserveResult.Err != nil {
		return operation.Reservation{}, false, f.operationReserveResult
	}
	if f.reservedOperation.OperationID != "" {
		if !contextChildReservationMatches(f.reservedOperation, want) {
			return f.reservedOperation, false, MutationResult{Durability: DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": string(want.OperationID)}, nil)}
		}
		return f.reservedOperation, false, MutationResult{Durability: DurableChange}
	}
	f.reservedOperation = want
	return want, true, f.operationReserveResult
}

func (f *admissionStoreFake) ReadOutput(_ context.Context, _ operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	if cursor < 0 || cursor > int64(len(f.output)) {
		return nil, 0, errors.New("cursor_out_of_range")
	}
	end := len(f.output)
	if max >= 0 && int(cursor)+max < end {
		end = int(cursor) + max
	}
	return append([]byte(nil), f.output[cursor:end]...), int64(end), nil
}

func (f *admissionStoreFake) AppendOutput(_ context.Context, _ operation.SessionID, data []byte) (int, MutationResult) {
	if f.events != nil {
		*f.events = append(*f.events, "persist_output")
	}
	f.output = append(f.output, data...)
	return len(data), MutationResult{Durability: DurableChange}
}

func (f *admissionStoreFake) PublishTerminal(_ context.Context, rec receipt.Receipt) MutationResult {
	if f.events != nil {
		*f.events = append(*f.events, "publish_receipt")
	}
	f.publishedReceipt = rec
	return MutationResult{Durability: DurableChange}
}

func (f *admissionStoreFake) ReserveContextExec(_ context.Context, want operation.ContextExecState) (operation.ContextExecState, bool, MutationResult) {
	f.reserveCalls++
	f.reservedWant = want.Clone()
	if f.events != nil {
		*f.events = append(*f.events, "reserve_context")
	}
	if f.reserveResult.Durability == "" {
		f.reserveResult.Durability = DurableChange
	}
	if f.reserveResult.Err != nil {
		return operation.ContextExecState{}, false, f.reserveResult
	}
	f.state, f.found = want.Clone(), true
	return f.state.Clone(), true, f.reserveResult
}

func (f *admissionStoreFake) AdvanceContextExec(_ context.Context, _ string, transition operation.ContextExecTransition) (operation.ContextExecState, MutationResult) {
	f.advanceCalls++
	f.transitions = append(f.transitions, transition)
	if f.events != nil {
		event := "advance_" + string(transition.Lifecycle)
		if transition.Lifecycle == core.LifecycleChildReserved && transition.ExecutionAuthorized {
			event = "authorize_child"
		}
		*f.events = append(*f.events, event)
	}
	if f.advanceResult.Durability == "" {
		f.advanceResult.Durability = DurableChange
	}
	if f.advanceResult.Err != nil {
		return f.state.Clone(), f.advanceResult
	}
	f.state.Lifecycle = transition.Lifecycle
	f.state.UpdatedAt = f.state.UpdatedAt.Add(time.Second)
	if transition.Helper != nil {
		h := *transition.Helper
		f.state.Helper = &h
	}
	if transition.ChildOperationID != "" || transition.ChildSessionID != "" {
		f.state.ChildOperationID, f.state.ChildSessionID = transition.ChildOperationID, transition.ChildSessionID
	}
	if transition.ExecutionAuthorized {
		f.state.ExecutionAuthorized = true
	}
	if transition.Result != nil {
		r := *transition.Result
		f.state.Result = &r
	}
	return f.state.Clone(), f.advanceResult
}

func (f *admissionStoreFake) BindHelperGeneration(_ context.Context, _ string, helper core.HelperBinding, finalContext core.ContextBinding, boundaryObservedAt time.Time, verifierDigest string) (operation.ContextExecState, MutationResult) {
	f.bindCalls++
	f.boundHelper, f.boundContext, f.boundAt, f.boundVerifier = helper, finalContext, boundaryObservedAt, verifierDigest
	if f.events != nil {
		*f.events = append(*f.events, "bind_helper")
	}
	if f.bindResult.Durability == "" {
		f.bindResult.Durability = DurableChange
	}
	if f.bindResult.Err != nil {
		return f.state.Clone(), f.bindResult
	}
	contextCopy := finalContext
	f.state.Context = &contextCopy
	f.state.BoundaryObservedAt = boundaryObservedAt
	f.state.Lifecycle = core.LifecycleHelperAuthenticated
	f.state.UpdatedAt = boundaryObservedAt
	return f.state.Clone(), f.bindResult
}

func (f *admissionStoreFake) AcquireContextExecLease(_ context.Context, sessionID operation.SessionID, epoch delegated.AuthorityEpoch, contextExecID, fingerprint string) (operation.ContextExecLease, bool, MutationResult) {
	f.acquireCalls++
	if f.events != nil {
		*f.events = append(*f.events, "acquire_lease")
	}
	if f.acquireResult.Durability == "" {
		f.acquireResult.Durability = DurableChange
	}
	if f.acquireResult.Err != nil {
		return operation.ContextExecLease{}, false, f.acquireResult
	}
	lease := operation.ContextExecLease{SessionID: sessionID, AuthorityEpoch: epoch, ContextExecID: contextExecID, RequestFingerprint: fingerprint}
	f.lease, f.leaseFound = lease, true
	return lease, true, f.acquireResult
}

func (f *admissionStoreFake) ReleaseContextExecLease(context.Context, operation.ContextExecLease) MutationResult {
	f.releaseCalls++
	if f.events != nil {
		*f.events = append(*f.events, "release_lease")
	}
	if f.releaseResult.Durability == "" {
		f.releaseResult.Durability = DurableChange
	}
	f.leaseFound = false
	return f.releaseResult
}

func (f *admissionStoreFake) ListContextExecRecoveryCandidates(context.Context) ([]operation.ContextExecState, error) {
	f.recoveryListCalls++
	if f.recoveryErr != nil {
		return nil, f.recoveryErr
	}
	out := make([]operation.ContextExecState, len(f.recoveryCandidates))
	for i := range f.recoveryCandidates {
		out[i] = f.recoveryCandidates[i].Clone()
	}
	return out, nil
}

type authorityFake struct {
	snapshot AuthoritySnapshot
	err      error
	calls    int
}

func (f *authorityFake) Snapshot(context.Context, core.Request) (AuthoritySnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

type helperRuntimeFake struct {
	qualified bool
	armCalls  int
	arm       shellapp.ContextHelperArm
	armErr    error
	events    *[]string
}

func (f *helperRuntimeFake) Qualified() bool { return f != nil && f.qualified }
func (f *helperRuntimeFake) ArmContextHelper(_ context.Context, armReq HelperArmRequest) (shellapp.ContextHelperArm, error) {
	req := armReq.Shell
	f.armCalls++
	if f.events != nil {
		*f.events = append(*f.events, "arm")
	}
	if f.armErr != nil {
		return shellapp.ContextHelperArm{}, f.armErr
	}
	if f.arm.ContextExecID == "" {
		f.arm = shellapp.ContextHelperArm{ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.Authority.Epoch, ProviderGeneration: req.Facts.ProviderGeneration, Shell: req.ExpectedShell, PaneShellPID: req.Facts.PanePID, PaneTTY: req.Facts.PaneTTY, OpaqueLaunchID: req.OpaqueLaunchID, ArmedAt: time.Date(2026, 8, 21, 14, 0, 2, 0, time.UTC)}
	}
	return f.arm, nil
}

func TestExecuteReplayFirstAvoidsFreshAuthorityObservationAndHelperArm(t *testing.T) {
	req := admissionRequest()
	state := helperRequestedState(t, req)
	store := &admissionStoreFake{state: state, found: true}
	authority := &authorityFake{snapshot: admissionAuthority(t, req)}
	helper := &helperRuntimeFake{qualified: true}
	svc := NewService(Options{Store: store, Authority: authority, Helper: helper, Now: func() time.Time { return state.CreatedAt }})

	got, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestFingerprint != state.RequestFingerprint || got.Lifecycle != state.Lifecycle {
		t.Fatalf("replay state=%#v", got)
	}
	if authority.calls != 0 || helper.armCalls != 0 || store.leaseReads != 0 {
		t.Fatalf("replay performed fresh admission authority=%d arm=%d lease=%d", authority.calls, helper.armCalls, store.leaseReads)
	}

	changed := req.Clone()
	changed.Argv = []string{"go", "test", "./changed"}
	if _, err := svc.Execute(context.Background(), changed); !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("changed replay err=%v want operation_conflict", err)
	}
	if authority.calls != 0 || helper.armCalls != 0 || store.leaseReads != 0 {
		t.Fatalf("conflict performed fresh admission authority=%d arm=%d lease=%d", authority.calls, helper.armCalls, store.leaseReads)
	}
}

func TestExecuteAdmissionRejectsEveryUnprovenGateBeforeHelperArm(t *testing.T) {
	req := admissionRequest()
	base := admissionAuthority(t, req)
	cases := []struct {
		name          string
		code          failure.Code
		mutate        func(*AuthoritySnapshot, *admissionStoreFake, *helperRuntimeFake)
		authorityCall int
	}{
		{name: "runtime unavailable", code: failure.ContextExecUnavailable, authorityCall: 0, mutate: func(_ *AuthoritySnapshot, _ *admissionStoreFake, h *helperRuntimeFake) { h.qualified = false }},
		{name: "session not live", code: failure.ContextExecUnavailable, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Binding.Lifecycle = delegated.LifecycleTerminal
		}},
		{name: "provider not current", code: failure.ContextExecStaleGeneration, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Observation.ProviderCurrent = false
		}},
		{name: "provider generation missing", code: failure.ContextExecStaleGeneration, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Observation.ProviderGeneration = ""
		}},
		{name: "provider generation drift", code: failure.ContextExecStaleGeneration, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.PrivacyProviderGeneration = "different_gen"
		}},
		{name: "owner not agent", code: failure.ContextExecNotAgentOwned, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Authority.Owner = delegated.OwnerHuman
			a.Authority.Fenced = true
		}},
		{name: "desired owner not agent", code: failure.ContextExecNotAgentOwned, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Binding.DesiredOwner = delegated.OwnerHuman
		}},
		{name: "agent ingress fenced", code: failure.ContextExecNotAgentOwned, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.AgentIngressWritable = false
		}},
		{name: "epoch changed", code: failure.ContextExecStaleGeneration, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) { a.Authority.Epoch++ }},
		{name: "privacy active", code: failure.ContextExecPrivacyBlocked, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) { a.PrivacyActive = true }},
		{name: "privacy release pending", code: failure.ContextExecPrivacyBlocked, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.PrivacyReleasePending = true
		}},
		{name: "ownership transfer active", code: failure.ContextExecNotAgentOwned, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.OwnershipTransferActive = true
		}},
		{name: "unknown shell", code: failure.ContextExecBoundaryUnproven, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Shell = shellcore.ShellIdentity{Family: shellcore.ShellUnknown, RuntimeID: "unknown_runtime"}
		}},
		{name: "pane tty missing", code: failure.ContextExecBoundaryUnproven, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) { a.Observation.PaneTTY = "" }},
		{name: "cwd not absolute", code: failure.ContextExecBoundaryUnproven, authorityCall: 1, mutate: func(a *AuthoritySnapshot, _ *admissionStoreFake, _ *helperRuntimeFake) {
			a.Observation.CWD = "relative"
		}},
		{name: "active context lease", code: failure.ContextExecAmbiguous, authorityCall: 1, mutate: func(_ *AuthoritySnapshot, s *admissionStoreFake, _ *helperRuntimeFake) {
			s.leaseFound = true
			s.lease = operation.ContextExecLease{SessionID: operation.SessionID(req.SessionID), AuthorityEpoch: req.AuthorityEpoch, ContextExecID: "ctxexec_other", RequestFingerprint: strings.Repeat("b", 64)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &admissionStoreFake{}
			authority := &authorityFake{snapshot: base}
			helper := &helperRuntimeFake{qualified: true}
			tc.mutate(&authority.snapshot, store, helper)
			svc := NewService(Options{Store: store, Authority: authority, Helper: helper, Now: time.Now})
			if _, err := svc.Execute(context.Background(), req); !errors.Is(err, tc.code) {
				t.Fatalf("err=%v want=%s", err, tc.code)
			}
			if helper.armCalls != 0 {
				t.Fatalf("helper armed on rejected admission: %d", helper.armCalls)
			}
			if authority.calls != tc.authorityCall {
				t.Fatalf("authority calls=%d want=%d", authority.calls, tc.authorityCall)
			}
		})
	}
}

func admissionRequest() core.Request {
	return core.Request{ContextExecID: "ctxexec_admission_01", SessionID: "session_context_01", AuthorityEpoch: 4, Argv: []string{"go", "test", "./internal/app/contextexec"}, TimeoutMS: 1000, MaxOutputBytes: 4096}
}

func admissionReservedState(t *testing.T, req core.Request) operation.ContextExecState {
	t.Helper()
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	state := operation.ContextExecState{SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: core.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_exact", ShellIdentity: "fish:fish_runtime_01", CWDObserved: "/tmp/project", PrivacyState: "standard"}, Lifecycle: core.LifecycleReserved, CreatedAt: at, UpdatedAt: at}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func admissionAuthority(t *testing.T, req core.Request) AuthoritySnapshot {
	t.Helper()
	at := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: req.SessionID, OperationID: "parent_operation_01", SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: req.AuthorityEpoch, DesiredOwner: delegated.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleLive, CreatedAt: at, UpdatedAt: at}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: req.SessionID, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: "provider_ref_01", CreatedAt: at, UpdatedAt: at}
	obs := delegatedapp.Observation{Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_exact", Owner: delegated.OwnerAgent, PanePID: 4242, CurrentCommand: "fish", PaneTTY: "/dev/ttys042", CWD: "/tmp/project"}
	return AuthoritySnapshot{Binding: binding, ProviderRef: ref, Observation: obs, Authority: delegated.EffectiveAuthority{Epoch: req.AuthorityEpoch, Owner: delegated.OwnerAgent}, PrivacyProviderGeneration: "gen_exact", AgentIngressWritable: true, Shell: shellcore.ShellIdentity{Family: shellcore.ShellFish, RuntimeID: "fish_runtime_01"}}
}

func TestExecuteReservesExpectationRevalidatesAndAcquiresLeaseBeforeOneShotArm(t *testing.T) {
	req := admissionRequest()
	events := []string{}
	store := &admissionStoreFake{events: &events}
	first := admissionAuthority(t, req)
	authority := &authoritySequenceFake{snapshots: []AuthoritySnapshot{first, first}}
	helper := &helperRuntimeFake{qualified: true, events: &events}
	at := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	svc := NewService(Options{
		Store: store, Authority: authority, Helper: helper, Now: func() time.Time { return at },
		NewOpaqueLaunchID:   func() string { events = append(events, "new_launch_id"); return "launch_task6_01" },
		NewHelperGeneration: func() string { events = append(events, "new_helper_generation"); return "helper_generation_task6_01" },
		HelperExecutable:    "/opt/shellbeam/bin/shellbeam",
	})
	got, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleHelperRequested || got.Helper == nil {
		t.Fatalf("state=%#v", got)
	}
	if store.reservedWant.Context != nil || !store.reservedWant.BoundaryObservedAt.IsZero() || store.reservedWant.Lifecycle != core.LifecycleReserved {
		t.Fatalf("reservation preclaimed boundary: %#v", store.reservedWant)
	}
	if store.reservedWant.Expectation.ProviderGeneration != "gen_exact" || store.reservedWant.Expectation.ShellIdentity != "fish:fish_runtime_01" || store.reservedWant.Expectation.CWDObserved != "/tmp/project" || store.reservedWant.Expectation.PrivacyState != "standard" {
		t.Fatalf("expectation=%#v", store.reservedWant.Expectation)
	}
	wantOrder := []string{"lookup", "find_lease", "reserve_context", "new_launch_id", "new_helper_generation", "acquire_lease", "advance_helper_requested", "arm"}
	if strings.Join(events, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("events=%v want=%v", events, wantOrder)
	}
	if authority.calls != 2 {
		t.Fatalf("authority calls=%d want=2", authority.calls)
	}
	if helper.armCalls != 1 || store.acquireCalls != 1 || store.advanceCalls != 1 {
		t.Fatalf("arm=%d acquire=%d advance=%d", helper.armCalls, store.acquireCalls, store.advanceCalls)
	}
}

type authoritySequenceFake struct {
	snapshots []AuthoritySnapshot
	calls     int
}

func (f *authoritySequenceFake) Snapshot(context.Context, core.Request) (AuthoritySnapshot, error) {
	if f.calls >= len(f.snapshots) {
		return AuthoritySnapshot{}, errors.New("unexpected authority snapshot")
	}
	got := f.snapshots[f.calls]
	f.calls++
	return got, nil
}

func TestExecuteSecondAuthorityDriftStopsBeforeLeaseAndArm(t *testing.T) {
	req := admissionRequest()
	base := admissionAuthority(t, req)
	cases := []struct {
		name   string
		code   failure.Code
		mutate func(*AuthoritySnapshot)
	}{
		{name: "provider generation", code: failure.ContextExecStaleGeneration, mutate: func(a *AuthoritySnapshot) {
			a.Observation.ProviderGeneration = "gen_changed"
			a.PrivacyProviderGeneration = "gen_changed"
		}},
		{name: "owner", code: failure.ContextExecNotAgentOwned, mutate: func(a *AuthoritySnapshot) { a.Authority.Owner = delegated.OwnerHuman; a.Authority.Fenced = true }},
		{name: "epoch", code: failure.ContextExecStaleGeneration, mutate: func(a *AuthoritySnapshot) { a.Authority.Epoch++ }},
		{name: "shell", code: failure.ContextExecBoundaryUnproven, mutate: func(a *AuthoritySnapshot) {
			a.Shell = shellcore.ShellIdentity{Family: shellcore.ShellZsh, RuntimeID: "zsh_changed"}
		}},
		{name: "tty", code: failure.ContextExecBoundaryUnproven, mutate: func(a *AuthoritySnapshot) { a.Observation.PaneTTY = "/dev/ttys099" }},
		{name: "cwd", code: failure.ContextExecBoundaryUnproven, mutate: func(a *AuthoritySnapshot) { a.Observation.CWD = "/tmp/changed" }},
		{name: "privacy", code: failure.ContextExecPrivacyBlocked, mutate: func(a *AuthoritySnapshot) { a.PrivacyActive = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			second := base
			tc.mutate(&second)
			store := &admissionStoreFake{}
			authority := &authoritySequenceFake{snapshots: []AuthoritySnapshot{base, second}}
			helper := &helperRuntimeFake{qualified: true}
			svc := NewService(Options{Store: store, Authority: authority, Helper: helper, Now: time.Now, NewOpaqueLaunchID: func() string { return "launch_task6_drift" }, NewHelperGeneration: func() string { return "helper_generation_task6_drift" }, HelperExecutable: "/opt/shellbeam/bin/shellbeam"})
			if _, err := svc.Execute(context.Background(), req); !errors.Is(err, tc.code) {
				t.Fatalf("err=%v want=%s", err, tc.code)
			}
			if store.reserveCalls != 1 || store.acquireCalls != 0 || store.advanceCalls != 0 || helper.armCalls != 0 {
				t.Fatalf("reserve=%d acquire=%d advance=%d arm=%d", store.reserveCalls, store.acquireCalls, store.advanceCalls, helper.armCalls)
			}
		})
	}
}

func TestExecuteArmFailureBecomesAmbiguousWithoutSecondHook(t *testing.T) {
	req := admissionRequest()
	base := admissionAuthority(t, req)
	store := &admissionStoreFake{}
	authority := &authoritySequenceFake{snapshots: []AuthoritySnapshot{base, base}}
	helper := &helperRuntimeFake{qualified: true, armErr: errors.New("shell write outcome unknown")}
	svc := NewService(Options{Store: store, Authority: authority, Helper: helper, Now: time.Now, NewOpaqueLaunchID: func() string { return "launch_task6_ambiguous" }, NewHelperGeneration: func() string { return "helper_generation_task6_ambiguous" }, HelperExecutable: "/opt/shellbeam/bin/shellbeam"})
	got, err := svc.Execute(context.Background(), req)
	if !errors.Is(err, failure.ContextExecAmbiguous) {
		t.Fatalf("err=%v want context_exec_ambiguous", err)
	}
	if got.Lifecycle != core.LifecycleAmbiguous {
		t.Fatalf("lifecycle=%q", got.Lifecycle)
	}
	if helper.armCalls != 1 {
		t.Fatalf("arm calls=%d", helper.armCalls)
	}
	if store.releaseCalls != 0 {
		t.Fatalf("ambiguous arm released lease")
	}
}

func TestBindClaimRevalidatesFreshAuthorityBeforeAtomicHelperBinding(t *testing.T) {
	req := admissionRequest()
	state := helperRequestedState(t, req)
	store := &admissionStoreFake{state: state, found: true}
	authority := &authorityFake{snapshot: admissionAuthority(t, req)}
	svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})
	final := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: state.Expectation.CWDObserved, PrivacyState: "standard"}
	at := time.Date(2026, 8, 21, 14, 5, 0, 0, time.UTC)
	verifier := strings.Repeat("c", 64)
	got, err := svc.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, at, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleHelperAuthenticated || got.Context == nil || *got.Context != final || !got.BoundaryObservedAt.Equal(at) {
		t.Fatalf("bound state=%#v", got)
	}
	if store.bindCalls != 1 || store.boundHelper != *state.Helper || store.boundContext != final || store.boundVerifier != verifier || !store.boundAt.Equal(at) {
		t.Fatalf("bind call mismatch")
	}
	if authority.calls != 1 {
		t.Fatalf("authority calls=%d", authority.calls)
	}
}

func TestBindClaimRejectsContextOrAuthorityDriftBeforeStoreBinding(t *testing.T) {
	req := admissionRequest()
	baseState := helperRequestedState(t, req)
	baseAuthority := admissionAuthority(t, req)
	baseContext := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: baseState.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: baseState.Expectation.CWDObserved, PrivacyState: "standard"}
	cases := []struct {
		name   string
		code   failure.Code
		mutate func(*AuthoritySnapshot, *core.ContextBinding, *core.HelperBinding)
	}{
		{name: "cwd", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *AuthoritySnapshot, c *core.ContextBinding, _ *core.HelperBinding) {
			c.CWDObserved = "/tmp/changed"
		}},
		{name: "shell context", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *AuthoritySnapshot, c *core.ContextBinding, _ *core.HelperBinding) {
			c.ShellIdentity = "zsh:changed"
		}},
		{name: "helper generation", code: failure.ContextHelperAuthFailed, mutate: func(_ *AuthoritySnapshot, _ *core.ContextBinding, h *core.HelperBinding) {
			h.Generation = "other_generation"
		}},
		{name: "provider generation", code: failure.ContextExecStaleGeneration, mutate: func(a *AuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding) {
			a.Observation.ProviderGeneration = "other_gen"
			a.PrivacyProviderGeneration = "other_gen"
		}},
		{name: "shell authority", code: failure.ContextExecBoundaryUnproven, mutate: func(a *AuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding) {
			a.Shell = shellcore.ShellIdentity{Family: shellcore.ShellZsh, RuntimeID: "zsh_runtime"}
		}},
		{name: "privacy", code: failure.ContextExecPrivacyBlocked, mutate: func(a *AuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding) { a.PrivacyActive = true }},
		{name: "owner", code: failure.ContextExecNotAgentOwned, mutate: func(a *AuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding) {
			a.Authority.Owner = delegated.OwnerHuman
			a.Authority.Fenced = true
		}},
		{name: "epoch", code: failure.ContextExecStaleGeneration, mutate: func(a *AuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding) { a.Authority.Epoch++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := baseState.Clone()
			authoritySnapshot := baseAuthority
			final := baseContext
			helper := *state.Helper
			tc.mutate(&authoritySnapshot, &final, &helper)
			store := &admissionStoreFake{state: state, found: true}
			svc := NewService(Options{Store: store, Authority: &authorityFake{snapshot: authoritySnapshot}, Helper: &helperRuntimeFake{qualified: true}})
			_, err := svc.BindClaim(context.Background(), req.ContextExecID, helper, final, time.Now(), strings.Repeat("d", 64))
			if !errors.Is(err, tc.code) {
				t.Fatalf("err=%v want=%s", err, tc.code)
			}
			if store.bindCalls != 0 {
				t.Fatalf("store bind called on drift")
			}
		})
	}
}

func helperRequestedState(t *testing.T, req core.Request) operation.ContextExecState {
	t.Helper()
	state := admissionReservedState(t, req)
	helper := core.HelperBinding{OpaqueLaunchID: "launch_claim_task6", Generation: "helper_generation_claim_task6", RequestFingerprint: state.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	state.Helper = &helper
	state.Lifecycle = core.LifecycleHelperRequested
	state.UpdatedAt = state.UpdatedAt.Add(time.Second)
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAuthorizePreparedReservesExactChildBeforeExecutionAuthorization(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	events := []string{}
	store := &admissionStoreFake{state: state, found: true, events: &events}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})

	got, authorization, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	wantOp, wantSession, err := operation.DeriveContextChildIDs(state.RequestFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleChildReserved || !got.ExecutionAuthorized || got.ChildOperationID != wantOp || got.ChildSessionID != wantSession {
		t.Fatalf("authorized state=%#v", got)
	}
	if authorization.ChildOperationID != wantOp || authorization.ChildSessionID != wantSession || authorization.ResolvedExecutable != "/usr/bin/go" {
		t.Fatalf("authorization=%#v", authorization)
	}
	if got := strings.Join(events, ","); got != "reserve_operation,advance_child_reserved,authorize_child" {
		t.Fatalf("durable ordering=%q", got)
	}
	reservation := store.reservedOperation
	if reservation.SchemaVersion != operation.ContextExecReservationSchemaVersion || reservation.OperationID != wantOp || reservation.SessionID != wantSession || reservation.RequestFingerprint != state.RequestFingerprint || reservation.ExecutionMode != operation.ExecutionModeArgv || reservation.Executable != "/usr/bin/go" || reservation.CWD != state.Context.CWDObserved || reservation.TimeoutMS != state.Request.TimeoutMS || reservation.DaemonIncarnation != "daemon_task6" {
		t.Fatalf("reservation=%#v", reservation)
	}
	if !slices.Equal(reservation.Argv, state.Request.Argv) {
		t.Fatalf("reservation argv=%q want=%q", reservation.Argv, state.Request.Argv)
	}
	if reservation.ContextExec == nil || reservation.ContextExec.ContextExecID != req.ContextExecID || reservation.ContextExec.ParentSessionID != operation.SessionID(req.SessionID) || reservation.ContextExec.AuthorityEpoch != req.AuthorityEpoch || reservation.ContextExec.RequestFingerprint != state.RequestFingerprint {
		t.Fatalf("context binding=%#v", reservation.ContextExec)
	}
	wantFP, err := reservation.ContextExec.ExecutionFingerprint(state.Context.CWDObserved, "/usr/bin/go")
	if err != nil || reservation.ExecutionFingerprint != wantFP {
		t.Fatalf("execution fingerprint=%q want=%q err=%v", reservation.ExecutionFingerprint, wantFP, err)
	}
}

func TestAuthorizePreparedRejectsChangedExecutableThroughExactReservationConflict(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	store := &admissionStoreFake{state: state, found: true, operationReserveResult: MutationResult{Durability: DurableChange, Err: failure.New(failure.OperationConflict, map[string]string{"operation_id": "child"}, nil)}}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})
	if _, _, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/changed"); !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("err=%v want operation_conflict", err)
	}
	if store.advanceCalls != 0 {
		t.Fatalf("child state advanced after reservation conflict")
	}
}

func helperAuthenticatedState(t *testing.T, req core.Request) operation.ContextExecState {
	t.Helper()
	state := helperRequestedState(t, req)
	final := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: state.Expectation.CWDObserved, PrivacyState: state.Expectation.PrivacyState}
	state.Context = &final
	state.BoundaryObservedAt = state.UpdatedAt.Add(time.Second)
	state.UpdatedAt = state.BoundaryObservedAt
	state.Lifecycle = core.LifecycleHelperAuthenticated
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRecordSpawnPersistsExactSuccessfulTruthAndRetainsLease(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	store := &admissionStoreFake{state: state, found: true}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})
	authorized, authorization, err := svc.AuthorizePrepared(context.Background(), state, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	store.events = &[]string{}
	got, err := svc.RecordSpawn(context.Background(), authorized, SpawnTruth{
		ChildOperationID: authorization.ChildOperationID, ChildSessionID: authorization.ChildSessionID,
		ResolvedExecutable: authorization.ResolvedExecutable,
		Spawn:              receipt.SpawnEvidence{Attempted: true, Succeeded: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleChildSpawned || !got.ExecutionAuthorized || got.ChildOperationID != authorization.ChildOperationID || got.ChildSessionID != authorization.ChildSessionID {
		t.Fatalf("spawned state=%#v", got)
	}
	if store.releaseCalls != 0 {
		t.Fatalf("successful spawn released execution lease")
	}
	if events := strings.Join(*store.events, ","); events != "reserve_operation,advance_child_spawned" {
		t.Fatalf("spawn persistence ordering=%q", events)
	}
}

func TestRecordSpawnRejectsExecutableOrChildIdentityDriftBeforeLifecycleAdvance(t *testing.T) {
	req := admissionRequest()
	base := helperAuthenticatedState(t, req)
	store := &admissionStoreFake{state: base, found: true}
	svc := NewService(Options{Store: store, DaemonIncarnation: "daemon_task6"})
	authorized, authorization, err := svc.AuthorizePrepared(context.Background(), base, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*SpawnTruth)
	}{
		{name: "operation id", mutate: func(v *SpawnTruth) { v.ChildOperationID = "cxop_other" }},
		{name: "session id", mutate: func(v *SpawnTruth) { v.ChildSessionID = "cxs_other" }},
		{name: "executable", mutate: func(v *SpawnTruth) { v.ResolvedExecutable = "/usr/bin/other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store.state = authorized.Clone()
			store.advanceCalls = 0
			truth := SpawnTruth{ChildOperationID: authorization.ChildOperationID, ChildSessionID: authorization.ChildSessionID, ResolvedExecutable: authorization.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}
			tc.mutate(&truth)
			if _, err := svc.RecordSpawn(context.Background(), authorized, truth); err == nil {
				t.Fatal("spawn drift accepted")
			}
			if store.advanceCalls != 0 {
				t.Fatal("spawn drift advanced lifecycle")
			}
		})
	}
}

func TestExecuteReservedReplayContinuesAdmissionOnlyAfterExactFingerprintMatch(t *testing.T) {
	req := admissionRequest()
	reserved := admissionReservedState(t, req)
	base := admissionAuthority(t, req)
	store := &admissionStoreFake{state: reserved, found: true}
	authority := &authoritySequenceFake{snapshots: []AuthoritySnapshot{base, base}}
	helper := &helperRuntimeFake{qualified: true}
	svc := NewService(Options{Store: store, Authority: authority, Helper: helper, Now: time.Now, NewOpaqueLaunchID: func() string { return "launch_replay_reserved" }, NewHelperGeneration: func() string { return "helper_generation_replay_reserved" }, HelperExecutable: "/opt/shellbeam/bin/shellbeam", DaemonIncarnation: "daemon_task6"})
	got, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleHelperRequested {
		t.Fatalf("lifecycle=%q", got.Lifecycle)
	}
	if store.reserveCalls != 0 || authority.calls != 2 || helper.armCalls != 1 {
		t.Fatalf("reserve=%d authority=%d arm=%d", store.reserveCalls, authority.calls, helper.armCalls)
	}
}
