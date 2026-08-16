//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestRunRejectsInheritedCapabilityMismatchBeforeChildSpawn(t *testing.T) {
	stored, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if _, err := PreparePrivateState(runtimeRoot, "runner-cap", "generation-cap", stored); err != nil {
		t.Fatal(err)
	}
	bootstrap := Bootstrap{
		SchemaVersion: BootstrapSchemaVersion, RuntimeRoot: runtimeRoot, SessionID: "runner-cap", GenerationID: "generation-cap",
		Execution:      BootstrapExecution{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"},
		MaxOutputBytes: 1024, MaxQueuedInputBytes: 128, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGraceMS: 25,
	}
	owner := newRuntimeFakeOwner()
	err = Run(context.Background(), bootstrap, other, owner)
	if !errors.Is(err, failure.SupervisorAuthFailed) {
		t.Fatalf("Run mismatch err=%v", err)
	}
	if owner.starts != 0 {
		t.Fatalf("capability mismatch spawned child %d times", owner.starts)
	}
}
