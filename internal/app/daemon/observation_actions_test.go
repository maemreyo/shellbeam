package daemon_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	appenv "github.com/maemreyo/shellbeam/internal/app/environment"
	appprocess "github.com/maemreyo/shellbeam/internal/app/process"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type fakeEnvironmentInspector struct{ calls int }

func (f *fakeEnvironmentInspector) Inspect(context.Context, appenv.InspectRequest) (environment.Snapshot, error) {
	f.calls++
	return environment.Snapshot{SchemaVersion: environment.SnapshotSchemaVersion}, nil
}

type fakeProcessInspector struct{ calls int }

func (f *fakeProcessInspector) Inspect(context.Context, appprocess.Request) (processcore.Observation, error) {
	f.calls++
	return processcore.Observation{SchemaVersion: processcore.SchemaVersion}, nil
}

func TestDaemonObservationActionsAreExplicitAndOptional(t *testing.T) {
	svc := app.NewService(nil, nil, app.Options{})
	env := &fakeEnvironmentInspector{}
	proc := &fakeProcessInspector{}
	svc.SetObservationInspectors(env, proc)
	if env.calls != 0 || proc.calls != 0 {
		t.Fatal("setting inspectors performed observation work")
	}
	if _, err := svc.InspectEnvironment(context.Background(), appenv.InspectRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InspectProcess(context.Background(), appprocess.Request{Target: processcore.Target{Kind: processcore.TargetPID, PID: 1}}); err != nil {
		t.Fatal(err)
	}
	if env.calls != 1 || proc.calls != 1 {
		t.Fatalf("explicit calls env=%d process=%d", env.calls, proc.calls)
	}
	_ = time.Second
}

func TestOrdinaryStartPollNeverInvokeA25Inspectors(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	wait := make(chan struct{})
	svc := app.NewService(store, &pidOwner{pid: 4343, wait: wait}, app.Options{Incarnation: "a25-no-tax", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	env := &fakeEnvironmentInspector{}
	proc := &fakeProcessInspector{}
	svc.SetObservationInspectors(env, proc)

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "a25-no-tax", Command: "sleep", CWD: "/", YieldMS: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Poll(context.Background(), app.PollRequest{SessionID: started.SessionID, YieldMS: 1}); err != nil {
		t.Fatal(err)
	}
	if env.calls != 0 || proc.calls != 0 {
		t.Fatalf("ordinary start/poll triggered A2.5 observation: env=%d process=%d", env.calls, proc.calls)
	}
	close(wait)
	_ = waitForTerminal(t, svc, started.SessionID)
	if env.calls != 0 || proc.calls != 0 {
		t.Fatalf("terminalization triggered A2.5 observation: env=%d process=%d", env.calls, proc.calls)
	}
}
