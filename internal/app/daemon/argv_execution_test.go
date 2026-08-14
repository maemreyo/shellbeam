package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type capturingOwner struct {
	starts atomic.Int32
	mu     sync.Mutex
	specs  []operation.ExecutionSpec
}

func (o *capturingOwner) BindExecution(spec operation.ExecutionSpec) operation.ExecutionSpec {
	if spec.Mode == operation.ExecutionModeArgv && len(spec.Argv) > 0 {
		spec.Executable = spec.Argv[0]
	} else {
		spec.Executable = spec.Shell
	}
	return spec
}

func (o *capturingOwner) Start(_ context.Context, spec operation.ExecutionSpec, _ app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.starts.Add(1)
	o.mu.Lock()
	o.specs = append(o.specs, spec)
	o.mu.Unlock()
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func TestArgvStartBindsExactSpecAndReceiptMode(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &capturingOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	argv := []string{"/bin/echo", "a b", "", "--flag"}
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-argv", Argv: argv, CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	owner.mu.Lock()
	specs := append([]operation.ExecutionSpec(nil), owner.specs...)
	owner.mu.Unlock()
	if len(specs) != 1 || specs[0].Mode != operation.ExecutionModeArgv || len(specs[0].Argv) != len(argv) {
		t.Fatalf("specs=%#v", specs)
	}
	for i := range argv {
		if specs[0].Argv[i] != argv[i] {
			t.Fatalf("argv=%#v", specs[0].Argv)
		}
	}
	if terminal.Receipt == nil || terminal.Receipt.ExecutionMode != "argv" || terminal.Receipt.Executable != "/bin/echo" || terminal.Receipt.Shell != "" {
		t.Fatalf("receipt=%#v", terminal.Receipt)
	}
	retry := app.StartRequest{ProtocolVersion: 2, OperationID: "op-argv", Argv: argv, CWD: "/", Intent: &operation.DeclaredIntent{Kind: operation.IntentKindTest}}
	if _, err := svc.Start(context.Background(), retry); !errors.Is(err, failure.OperationMetadataConflict) {
		t.Fatalf("intent conflict err=%v", err)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}
