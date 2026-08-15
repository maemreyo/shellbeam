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

func TestCreateTypedProjectCommandRedactsUnsafeFrozenArgvButKeepsBindingFacts(t *testing.T) {
	repo := reproFixture(t)
	binding := reproProjectCommandBinding(t)
	binding.ResolvedArgv = []string{"go", "test", "--token=super-secret"}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	repo.receipt.SchemaVersion = 3
	repo.receipt.ProjectCommand = &binding
	repo.receipt.CWD = binding.ResolvedCWD
	repo.reservation.SchemaVersion = 3
	repo.reservation.ProjectCommand = &binding
	repo.reservation.Argv = append([]string(nil), binding.ResolvedArgv...)
	repo.reservation.LogicalCWD = binding.LogicalCWD
	repo.reservation.CWD = binding.ResolvedCWD
	repo.telemetryFound = false

	capsule, err := New(repo).Create(context.Background(), core.CreateRequest{CreateID: "repro-typed-private-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "--token=super-secret") {
		t.Fatalf("typed capsule leaked frozen argv: %s", encoded)
	}
	if len(capsule.Execution.ResolvedArgv) != 0 || capsule.Execution.CommandDetails != core.CapturePartial {
		t.Fatalf("unsafe typed argv was not redacted: %#v", capsule.Execution)
	}
	if capsule.Execution.ProjectCommandBindingDigest == "" || capsule.Execution.ProjectManifestDigest != binding.ManifestDigest || capsule.Execution.ProjectCommandID != binding.CommandID {
		t.Fatalf("typed binding facts were lost during redaction: %#v", capsule.Execution)
	}
	if got := capsule.Execution.ParameterProviders; !reflect.DeepEqual(got, []core.ParameterProviderDescriptor{{ParameterID: "package", ProviderID: "go-repo-package", ProviderVersion: 1}}) {
		t.Fatalf("provider facts=%#v", got)
	}
}
