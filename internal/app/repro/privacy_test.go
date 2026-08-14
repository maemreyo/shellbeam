package repro

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

func TestCreateOmitsUnsafeCommandMaterialButRetainsSafeArgv(t *testing.T) {
	unsafeRepo := reproFixture(t)
	unsafeRepo.reservation.Executable = "/usr/bin/tool"
	unsafeRepo.receipt.Executable = "/usr/bin/tool"
	unsafeRepo.reservation.Argv = []string{"tool", "--token=super-secret", "password=hunter2", "-----BEGIN PRIVATE KEY-----", "/Users/alice/.ssh/id_work"}
	unsafe, err := New(unsafeRepo).Create(context.Background(), core.CreateRequest{CreateID: "repro-unsafe-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"--token=super-secret", "password=hunter2", "-----BEGIN PRIVATE KEY-----", "/Users/alice/.ssh/id_work"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("capsule leaked %q: %s", forbidden, encoded)
		}
	}
	if len(unsafe.Execution.ResolvedArgv) != 0 || unsafe.Execution.CommandDetails != core.CapturePartial || unsafe.Execution.Executable != "/usr/bin/tool" {
		t.Fatalf("unsafe command descriptor=%#v", unsafe.Execution)
	}

	safeRepo := reproFixture(t)
	safe, err := New(safeRepo).Create(context.Background(), core.CreateRequest{CreateID: "repro-safe-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	if safe.Execution.CommandDetails != core.CaptureExact || !reflect.DeepEqual(safe.Execution.ResolvedArgv, []string{"go", "test", "./..."}) {
		t.Fatalf("safe argv not retained: %#v", safe.Execution)
	}
}
