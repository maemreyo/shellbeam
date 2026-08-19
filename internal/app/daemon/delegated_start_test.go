package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/activity"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type delegatedWaitResult struct {
	observation delegatedapp.Observation
	err         error
}

type delegatedStartRuntime struct {
	probes   atomic.Int32
	refs     atomic.Int32
	creates  atomic.Int32
	writes   atomic.Int32
	signals  atomic.Int32
	inspects atomic.Int32
	waits    atomic.Int32
	mu       sync.Mutex
	created  delegatedapp.CreateRequest
	waitCh   chan delegatedWaitResult
}

func newDelegatedStartRuntime() *delegatedStartRuntime {
	return &delegatedStartRuntime{waitCh: make(chan delegatedWaitResult, 1)}
}
func (*delegatedStartRuntime) Identity() delegated.ProviderIdentity {
	return delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
}
func (r *delegatedStartRuntime) ProviderRefForSession(sessionID string, at time.Time) (delegated.ProviderRef, error) {
	r.refs.Add(1)
	return delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: sessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "ref_" + sessionID, CreatedAt: at, UpdatedAt: at}, nil
}
func (r *delegatedStartRuntime) Probe(context.Context) error { r.probes.Add(1); return nil }
func (r *delegatedStartRuntime) Create(_ context.Context, req delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	r.creates.Add(1)
	r.mu.Lock()
	r.created = req
	r.mu.Unlock()
	if req.Output != nil {
		if err := req.Output.Append([]byte("delegated-ready\n")); err != nil {
			return delegatedapp.CreateResult{}, err
		}
	}
	return delegatedapp.CreateResult{ProviderRef: req.ProviderRef, Observation: delegatedapp.Observation{Provider: r.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerAgent}}, nil
}
func (*delegatedStartRuntime) Reattach(context.Context, delegated.ProviderRef, delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, nil
}
func (r *delegatedStartRuntime) Write(context.Context, delegated.ProviderRef, []byte) error {
	r.writes.Add(1)
	return nil
}
func (r *delegatedStartRuntime) Signal(context.Context, delegated.ProviderRef, string) error {
	r.signals.Add(1)
	return nil
}
func (r *delegatedStartRuntime) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	r.inspects.Add(1)
	return delegatedapp.Observation{Provider: r.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test", Owner: delegated.OwnerAgent}, nil
}
func (r *delegatedStartRuntime) Wait(ctx context.Context, _ delegated.ProviderRef) (delegatedapp.Observation, error) {
	r.waits.Add(1)
	select {
	case got := <-r.waitCh:
		return got.observation, got.err
	case <-ctx.Done():
		return delegatedapp.Observation{}, ctx.Err()
	}
}
func (*delegatedStartRuntime) Close(context.Context, delegated.ProviderRef) error { return nil }

func openDelegatedStartStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func delegatedStartRequest() app.StartRequest {
	return app.StartRequest{ProtocolVersion: 2, OperationID: "op-delegated-start", Command: "cat", CWD: "/tmp", SessionMode: delegated.ModeDelegatedInteractive, MaxOutputBytes: 4096}
}

func TestDelegatedStartUnavailableAndUnknownModeFailBeforeReservationOrProvider(t *testing.T) {
	for name, req := range map[string]app.StartRequest{
		"unconfigured": delegatedStartRequest(),
		"unknown":      func() app.StartRequest { v := delegatedStartRequest(); v.SessionMode = "future_mode"; return v }(),
	} {
		t.Run(name, func(t *testing.T) {
			st := openDelegatedStartStore(t)
			runtime := newDelegatedStartRuntime()
			opts := app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100}
			if name == "unknown" {
				opts.DelegatedRuntime = runtime
			}
			svc := app.NewService(st, &fakeOwner{}, opts)
			_, err := svc.Start(context.Background(), req)
			if !errors.Is(err, failure.FeatureUnavailable) {
				t.Fatalf("error=%v want feature_unavailable", err)
			}
			if runtime.probes.Load() != 0 || runtime.refs.Load() != 0 || runtime.creates.Load() != 0 {
				t.Fatalf("provider touched: %#v", runtime)
			}
			if _, found, findErr := st.FindOperation(context.Background(), operation.ID(req.OperationID)); findErr != nil || found {
				t.Fatalf("reservation found=%v err=%v", found, findErr)
			}
		})
	}
}

func TestDelegatedStartRejectsLegacyEvidenceAndHardLimitsBeforeProvider(t *testing.T) {
	cases := map[string]func(*app.StartRequest){
		"tty":        func(v *app.StartRequest) { v.TTY = true },
		"persistent": func(v *app.StartRequest) { v.Persistent = true },
		"evidence": func(v *app.StartRequest) {
			v.Evidence = &evidence.Contract{VerificationKind: evidence.VerificationTest}
		},
		"limits": func(v *app.StartRequest) { v.ResourceLimits = &operation.ResourceLimits{MemoryBytes: 1 << 20} },
		"trace":  func(v *app.StartRequest) { v.TraceMode = "best_effort" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			st := openDelegatedStartStore(t)
			runtime := newDelegatedStartRuntime()
			svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
			req := delegatedStartRequest()
			req.OperationID += "-" + name
			mutate(&req)
			_, err := svc.Start(context.Background(), req)
			if err == nil {
				t.Fatal("invalid delegated start accepted")
			}
			if runtime.probes.Load() != 0 || runtime.refs.Load() != 0 || runtime.creates.Load() != 0 {
				t.Fatalf("provider touched: probes=%d refs=%d creates=%d", runtime.probes.Load(), runtime.refs.Load(), runtime.creates.Load())
			}
			if _, found, findErr := st.FindOperation(context.Background(), operation.ID(req.OperationID)); findErr != nil || found {
				t.Fatalf("reservation found=%v err=%v", found, findErr)
			}
		})
	}
}

func TestDelegatedStartReservesSchema5BindingBeforeProviderAndNeverUsesProcessOwner(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	view, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if view.SessionID == "" || view.State != "running" || view.AuthorityEpoch != 1 {
		t.Fatalf("view=%#v", view)
	}
	if view.Output != "delegated-ready\n" {
		t.Fatalf("output=%q", view.Output)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("process owner starts=%d", owner.starts.Load())
	}
	if runtime.probes.Load() != 1 || runtime.refs.Load() != 1 || runtime.creates.Load() != 1 {
		t.Fatalf("runtime calls probe=%d ref=%d create=%d", runtime.probes.Load(), runtime.refs.Load(), runtime.creates.Load())
	}

	reservation, err := st.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if reservation.SchemaVersion != 5 || reservation.SessionMode != delegated.ModeDelegatedInteractive || reservation.AuthorityEpoch != 1 || string(reservation.SessionID) != view.SessionID {
		t.Fatalf("reservation=%#v", reservation)
	}
	binding, err := st.LoadDelegatedBinding(context.Background(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Lifecycle != delegated.LifecycleLive || binding.AuthorityEpoch != 1 || binding.DesiredOwner != delegated.OwnerAgent {
		t.Fatalf("binding=%#v", binding)
	}
	ref, err := st.LoadDelegatedProviderRef(context.Background(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	created := runtime.created
	runtime.mu.Unlock()
	if created.ProviderRef != ref || created.SessionID != view.SessionID || created.OperationID != req.OperationID {
		t.Fatalf("create=%#v ref=%#v", created, ref)
	}
}

func TestTypedDelegatedStartUsesSchema5ProviderRouteWithoutProcessSpawn(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, owner, app.Options{
		Incarnation: "typed-delegated", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		ProjectCommandBinder: binder, DelegatedRuntime: runtime,
	})
	req := typedStartRequest("typed-delegated-op", "./internal/app")
	req.SessionMode = delegated.ModeDelegatedInteractive
	req.SessionName = "typed-shell"
	req.TimeoutMS = 0
	view, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("typed delegated start err=%#v public=%#v sequence=%v", err, failure.Public(err), sequence.snapshot())
	}
	if view.SessionID == "" || view.AuthorityEpoch != 1 || view.State != "running" {
		t.Fatalf("view=%#v", view)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("typed delegated used process owner: %d", owner.starts.Load())
	}
	if runtime.creates.Load() != 1 || runtime.refs.Load() != 1 {
		t.Fatalf("runtime create=%d refs=%d", runtime.creates.Load(), runtime.refs.Load())
	}
	stored, err := store.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 5 || stored.SessionMode != delegated.ModeDelegatedInteractive || stored.AuthorityEpoch != 1 || stored.ProjectCommand == nil || stored.ProjectCommand.CommandID != req.ProjectCommandID {
		t.Fatalf("stored=%#v", stored)
	}
	runtime.mu.Lock()
	created := runtime.created
	runtime.mu.Unlock()
	if created.Spec.Mode != operation.ExecutionModeArgv || len(created.Spec.Argv) == 0 || created.Spec.Argv[0] != "go" || created.SessionName != "typed-shell" {
		t.Fatalf("create=%#v", created)
	}
}

type nonDelegatedStore struct{ app.Store }

func TestDelegatedStartRequiresDelegatedPersistenceBeforeProviderOrReservation(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(&nonDelegatedStore{Store: st}, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-no-store-port"
	_, err := svc.Start(context.Background(), req)
	if !errors.Is(err, failure.PersistenceUnavailable) {
		t.Fatalf("err=%v want persistence_unavailable", err)
	}
	if runtime.probes.Load() != 0 || runtime.refs.Load() != 0 || runtime.creates.Load() != 0 {
		t.Fatalf("provider touched probes=%d refs=%d creates=%d", runtime.probes.Load(), runtime.refs.Load(), runtime.creates.Load())
	}
	if _, found, findErr := st.FindOperation(context.Background(), operation.ID(req.OperationID)); findErr != nil || found {
		t.Fatalf("reservation found=%v err=%v", found, findErr)
	}
}

type ambiguousCreateRuntime struct {
	*delegatedStartRuntime
	createCalls      atomic.Int32
	providerSessions atomic.Int32
	mu2              sync.Mutex
	ref              string
}

func newAmbiguousCreateRuntime() *ambiguousCreateRuntime {
	return &ambiguousCreateRuntime{delegatedStartRuntime: newDelegatedStartRuntime()}
}

func (r *ambiguousCreateRuntime) Create(ctx context.Context, req delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	call := r.createCalls.Add(1)
	if call == 1 {
		r.providerSessions.Add(1)
		r.mu2.Lock()
		r.ref = req.ProviderRef.Ref
		r.mu2.Unlock()
		if req.Output != nil {
			if err := req.Output.Append([]byte("ambiguous-pre-ack\n")); err != nil {
				return delegatedapp.CreateResult{}, err
			}
		}
		return delegatedapp.CreateResult{}, failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": req.SessionID, "provider_id": "tmux_control_mode", "reason": "response_lost_after_create"}, nil)
	}
	r.mu2.Lock()
	firstRef := r.ref
	r.mu2.Unlock()
	if req.ProviderRef.Ref != firstRef {
		return delegatedapp.CreateResult{}, failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": req.SessionID, "provider_id": req.ProviderRef.ProviderID, "provider_version": "1", "expected_provider_id": req.ProviderRef.ProviderID, "expected_provider_version": "1"}, nil)
	}
	return r.delegatedStartRuntime.Create(ctx, req)
}

func TestDelegatedStartRetryAfterProviderResponseLossResumesSameSessionAndRef(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newAmbiguousCreateRuntime()
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-create-response-loss"
	req.StdinMode = operation.StdinModeStream
	req.TimeoutMode = operation.TimeoutModeUnlimited
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("first ambiguous provider create unexpectedly succeeded")
	}
	stored, err := st.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := st.LoadDelegatedBinding(context.Background(), stored.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := st.LoadDelegatedProviderRef(context.Background(), stored.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Lifecycle != delegated.LifecycleProvisioning {
		t.Fatalf("binding=%#v", binding)
	}

	replayed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != string(stored.SessionID) || replayed.State != session.Running || replayed.AuthorityEpoch != 1 {
		t.Fatalf("replay=%#v stored=%#v", replayed, stored)
	}
	if runtime.createCalls.Load() != 2 || runtime.providerSessions.Load() != 1 {
		t.Fatalf("create calls=%d provider sessions=%d", runtime.createCalls.Load(), runtime.providerSessions.Load())
	}
	runtime.mu2.Lock()
	firstRef := runtime.ref
	runtime.mu2.Unlock()
	if firstRef != ref.Ref {
		t.Fatalf("first ref=%q durable ref=%q", firstRef, ref.Ref)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("process owner starts=%d", owner.starts.Load())
	}
}

type countingDelegatedActivityTracker struct{ calls atomic.Int32 }

func (t *countingDelegatedActivityTracker) Admit(context.Context, activity.Admission) (activity.Activity, error) {
	t.calls.Add(1)
	return activity.Activity{}, nil
}

func TestDelegatedCreateAmbiguityDoesNotDuplicateActivityAdmission(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newAmbiguousCreateRuntime()
	tracker := &countingDelegatedActivityTracker{}
	svc := app.NewServiceWithActivityTracker(st, &fakeOwner{}, tracker, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-activity-retry"
	req.ActivityID = "activity-delegated-retry"
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("first ambiguous create unexpectedly succeeded")
	}
	if tracker.calls.Load() != 0 {
		t.Fatalf("activity admitted before provider authority: %d", tracker.calls.Load())
	}
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if tracker.calls.Load() != 1 {
		t.Fatalf("activity admits=%d want 1", tracker.calls.Load())
	}
}

func TestTypedDelegatedResponseLossRetryUsesFrozenBindingWithoutRebinding(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	runtime := newAmbiguousCreateRuntime()
	svc := app.NewService(store, &typedOrderOwner{sequence: sequence}, app.Options{Incarnation: "typed-delegated-retry", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder, DelegatedRuntime: runtime})
	req := typedStartRequest("typed-delegated-retry-op", "./internal/app")
	req.SessionMode = delegated.ModeDelegatedInteractive
	req.SessionName = "typed-retry-shell"
	req.TimeoutMS = 0
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("first ambiguous create unexpectedly succeeded")
	}
	if binder.callCount() != 1 {
		t.Fatalf("binder calls after first=%d", binder.callCount())
	}
	stored, err := store.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != string(stored.SessionID) || replayed.State != session.Running || replayed.AuthorityEpoch != 1 {
		t.Fatalf("replay=%#v stored=%#v", replayed, stored)
	}
	if binder.callCount() != 1 {
		t.Fatalf("typed replay rebound project command: calls=%d", binder.callCount())
	}
	if runtime.createCalls.Load() != 2 || runtime.providerSessions.Load() != 1 {
		t.Fatalf("create calls=%d sessions=%d", runtime.createCalls.Load(), runtime.providerSessions.Load())
	}
}

func TestDelegatedUnsetExecutionPolicyResolvesInteractiveStreamAndUnbounded(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", DefaultTimeoutMS: 600000, MaxTimeoutMS: 3600000, MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-policy-default"
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	created := runtime.created
	runtime.mu.Unlock()
	if created.Spec.StdinMode != operation.StdinModeStream || created.Spec.TimeoutMS != 0 {
		t.Fatalf("spec=%#v", created.Spec)
	}
	stored, err := st.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.TimeoutMS != 0 {
		t.Fatalf("stored timeout=%d", stored.TimeoutMS)
	}
	if started.AuthorityEpoch != 1 {
		t.Fatalf("started=%#v", started)
	}
}

func TestDelegatedExplicitUnlimitedTimeoutIsAccepted(t *testing.T) {
	st := openDelegatedStartStore(t)
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", DefaultTimeoutMS: 600000, MaxTimeoutMS: 3600000, MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
	req := delegatedStartRequest()
	req.OperationID = "op-delegated-policy-unlimited"
	req.TimeoutMode = operation.TimeoutModeUnlimited
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	created := runtime.created
	runtime.mu.Unlock()
	if created.Spec.StdinMode != operation.StdinModeStream || created.Spec.TimeoutMS != 0 {
		t.Fatalf("spec=%#v", created.Spec)
	}
}

func TestDelegatedUnsupportedClosedStdinAndBoundedTimeoutFailBeforeProviderOrReservation(t *testing.T) {
	cases := map[string]func(*app.StartRequest){
		"closed_stdin":            func(v *app.StartRequest) { v.StdinMode = operation.StdinModeClosed },
		"finite_timeout":          func(v *app.StartRequest) { v.TimeoutMode = operation.TimeoutModeFinite; v.TimeoutMS = 1000 },
		"default_timeout":         func(v *app.StartRequest) { v.TimeoutMode = operation.TimeoutModeDefault },
		"implicit_finite_timeout": func(v *app.StartRequest) { v.TimeoutMS = 1000 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			st := openDelegatedStartStore(t)
			runtime := newDelegatedStartRuntime()
			svc := app.NewService(st, &fakeOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", DefaultTimeoutMS: 600000, MaxTimeoutMS: 3600000, MaxQueuedInputBytes: 100, DelegatedRuntime: runtime})
			req := delegatedStartRequest()
			req.OperationID = "op-delegated-policy-" + name
			mutate(&req)
			_, err := svc.Start(context.Background(), req)
			if err == nil {
				t.Fatal("unsupported delegated execution policy accepted")
			}
			if runtime.probes.Load() != 0 || runtime.refs.Load() != 0 || runtime.creates.Load() != 0 {
				t.Fatalf("provider touched probes=%d refs=%d creates=%d", runtime.probes.Load(), runtime.refs.Load(), runtime.creates.Load())
			}
			if _, found, findErr := st.FindOperation(context.Background(), operation.ID(req.OperationID)); findErr != nil || found {
				t.Fatalf("reservation found=%v err=%v", found, findErr)
			}
		})
	}
}

func TestTypedDelegatedExecutionPolicyIsBoundAndResolvesStreamUnbounded(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binder := &typedBinder{sequence: sequence, binding: daemonProjectBinding(t, []string{"go", "test", "./internal/app"})}
	runtime := newDelegatedStartRuntime()
	svc := app.NewService(store, &typedOrderOwner{sequence: sequence}, app.Options{Incarnation: "typed-policy", Shell: "/bin/sh", DefaultTimeoutMS: 600000, MaxTimeoutMS: 3600000, MaxQueuedInputBytes: 100, ProjectCommandBinder: binder, DelegatedRuntime: runtime})
	req := typedStartRequest("typed-delegated-policy", "./internal/app")
	req.SessionMode = delegated.ModeDelegatedInteractive
	req.SessionName = "typed-policy-shell"
	req.TimeoutMS = 0
	req.TimeoutMode = operation.TimeoutModeUnlimited
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatalf("typed policy err=%#v public=%#v", err, failure.Public(err))
	}
	runtime.mu.Lock()
	created := runtime.created
	runtime.mu.Unlock()
	if created.Spec.StdinMode != operation.StdinModeStream || created.Spec.TimeoutMS != 0 {
		t.Fatalf("spec=%#v", created.Spec)
	}
	claim, found, err := store.FindTypedIntent(context.Background(), operation.ID(req.OperationID))
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if claim.Intent.TimeoutMode != operation.TimeoutModeUnlimited || claim.Intent.StdinMode != operation.StdinModeUnset {
		t.Fatalf("claim intent=%#v", claim.Intent)
	}
}
