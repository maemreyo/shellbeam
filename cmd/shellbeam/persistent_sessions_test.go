package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func TestPersistentSessionCatalogAdvertisesConfiguredBounds(t *testing.T) {
	base := daemonCatalog(capability.Limits{LiveSessions: 4, SessionOutputBytes: 4096})
	got := persistentSessionCatalog(base, 4, 4096, 512)
	if got.Features[capability.FeatureNamedSessions] != capability.Available {
		t.Fatalf("named sessions availability=%q", got.Features[capability.FeatureNamedSessions])
	}
	if got.Limits.PersistentSessions != 4 || got.Limits.PersistentRecoverySpoolBytes != 4096 || got.Limits.PersistentQueuedInputBytes != 512 {
		t.Fatalf("persistent limits=%#v", got.Limits)
	}
	if !got.PersistentNonTTY || got.PersistentTTY || got.PersistentContinuity != "daemon_restart" {
		t.Fatalf("persistent capability projection=%#v", got)
	}
}

func TestPersistentSessionRuntimeCompositionHasNoPrivateRuntimeSideEffects(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := storeadapter.Open(stateDir, storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 4096, MaxTotalState: 1 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	limits := persistentapp.Limits{
		MaxOutputBytes: 4096, MaxQueuedInputBytes: 512,
		MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8,
		TerminationGrace: 25 * time.Millisecond,
	}
	runtime, err := newPersistentSessionRuntime(store, runtimeRoot, "/bin/echo", limits)
	if err != nil || runtime == nil {
		t.Fatalf("runtime=%v err=%v", runtime, err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "supervisors")); !os.IsNotExist(err) {
		t.Fatalf("composition touched private supervisor runtime: %v", err)
	}
}
