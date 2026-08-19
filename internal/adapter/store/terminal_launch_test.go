package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestTerminalLaunchStoreIsLazyUntilFirstReservation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, _, req, initial := h2HandoffFixture(t, root, "terminal-launch-lazy")
	launchDir := r.terminalLaunchDir()
	if _, err := os.Stat(launchDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused terminal launch state created eagerly: err=%v", err)
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	advanceHandoffToHumanConnecting(t, r, initial)
	if _, created, err := r.ReserveTerminalLaunch(context.Background(), terminalLaunchTestRecord(req.HandoffID)); err != nil || !created {
		t.Fatalf("reserve created=%v err=%v", created, err)
	}
	info, err := os.Stat(launchDir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("lazy launch dir info=%#v err=%v", info, err)
	}
}

func TestTerminalLaunchReplayRejectsSymlinkedLaunchDirectoryAfterReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, _, req, initial := h2HandoffFixture(t, root, "terminal-launch-symlink")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	advanceHandoffToHumanConnecting(t, r, initial)
	record := terminalLaunchTestRecord(req.HandoffID)
	if _, created, err := r.ReserveTerminalLaunch(context.Background(), record); err != nil || !created {
		t.Fatalf("reserve created=%v err=%v", created, err)
	}
	launchDir := r.terminalLaunchDir()
	shadow := filepath.Join(t.TempDir(), "shadow")
	if err := os.Rename(launchDir, shadow); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shadow, launchDir); err != nil {
		t.Fatal(err)
	}
	r = delegatedRepository(t, root, 64)
	if _, _, err := r.ReserveTerminalLaunch(context.Background(), record); err == nil {
		t.Fatal("symlinked terminal launch directory accepted on replay")
	}
}

func TestTerminalLaunchReserveRequiresDurableHumanConnectingAndReplaysExactIdentity(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "terminal-launch-reserve")
	record := terminalLaunchTestRecord(req.HandoffID)

	if _, _, err := r.ReserveTerminalLaunch(context.Background(), record); err == nil {
		t.Fatal("terminal launch reserved before durable H2 handoff")
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	if _, _, err := r.ReserveTerminalLaunch(context.Background(), record); err == nil {
		t.Fatal("terminal launch reserved before H2 agent fence completed")
	}
	advanceHandoffToHumanConnecting(t, r, initial)

	stored, created, err := r.ReserveTerminalLaunch(context.Background(), record)
	if err != nil || !created || stored != record {
		t.Fatalf("reserve stored=%#v created=%v err=%v", stored, created, err)
	}
	replay, created, err := r.ReserveTerminalLaunch(context.Background(), record)
	if err != nil || created || replay != record {
		t.Fatalf("replay=%#v created=%v err=%v", replay, created, err)
	}
	conflict := record
	conflict.AttachTargetFingerprint = strings.Repeat("c", 64)
	if _, created, err := r.ReserveTerminalLaunch(context.Background(), conflict); err == nil || created {
		t.Fatalf("conflicting attach target accepted created=%v err=%v", created, err)
	}
}

func TestTerminalLaunchConcurrentReserveCreatesExactlyOneClaim(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "terminal-launch-race")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	advanceHandoffToHumanConnecting(t, r, initial)
	record := terminalLaunchTestRecord(req.HandoffID)

	const workers = 12
	var wg sync.WaitGroup
	created := make(chan bool, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, wasCreated, err := r.ReserveTerminalLaunch(context.Background(), record)
			if err == nil && got != record {
				err = errors.New("terminal launch replay changed canonical record")
			}
			created <- wasCreated
			errs <- err
		}()
	}
	wg.Wait()
	close(created)
	close(errs)
	createdCount := 0
	for wasCreated := range created {
		if wasCreated {
			createdCount++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created claims=%d want=1", createdCount)
	}
}

func TestTerminalLaunchCompletionIsMonotonicAndSurvivesReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, _, req, initial := h2HandoffFixture(t, root, "terminal-launch-complete")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	advanceHandoffToHumanConnecting(t, r, initial)
	record := terminalLaunchTestRecord(req.HandoffID)
	if _, created, err := r.ReserveTerminalLaunch(context.Background(), record); err != nil || !created {
		t.Fatalf("reserve created=%v err=%v", created, err)
	}

	unknown := record
	unknown.State = core.LaunchOutcomeUnknownState
	unknown.FailureCode = failure.TerminalLaunchUnknown
	unknown.FailureReason = "client_not_proven"
	stored, err := r.CompleteTerminalLaunch(context.Background(), unknown)
	if err != nil || stored != unknown {
		t.Fatalf("complete unknown=%#v err=%v", stored, err)
	}

	r = delegatedRepository(t, root, 64)
	replay, created, err := r.ReserveTerminalLaunch(context.Background(), record)
	if err != nil || created || replay != unknown {
		t.Fatalf("reopen replay=%#v created=%v err=%v", replay, created, err)
	}
	proven := record
	proven.State = core.LaunchLaunchedAndClientProven
	stored, err = r.CompleteTerminalLaunch(context.Background(), proven)
	if err != nil || stored != proven {
		t.Fatalf("promote proven=%#v err=%v", stored, err)
	}
	failed := record
	failed.State = core.LaunchFailed
	failed.FailureCode = failure.TerminalLaunchFailed
	failed.FailureReason = "late_failure"
	if _, err := r.CompleteTerminalLaunch(context.Background(), failed); err == nil {
		t.Fatal("proven launch downgraded to failure")
	}
}

func TestTerminalLaunchFirstReservationFailsOnceExactHumanClientIsDurable(t *testing.T) {
	r, _, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "terminal-launch-client")
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); result.Err != nil {
		t.Fatal(result.Err)
	}
	connecting := advanceHandoffToHumanConnecting(t, r, initial)
	connecting.ProviderOwner = delegated.OwnerNone
	connecting.HumanClient = &handoff.HumanClientRef{Ref: "human-client-terminal-launch"}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	if _, _, err := r.ReserveTerminalLaunch(context.Background(), terminalLaunchTestRecord(req.HandoffID)); err == nil {
		t.Fatal("new GUI launch reserved after exact human client became durable")
	}
}

func advanceHandoffToHumanConnecting(t *testing.T, r *Repository, initial handoff.State) handoff.State {
	t.Helper()
	connecting := initial
	connecting.Phase = handoff.PhaseHumanConnecting
	connecting.AgentIngress = handoff.IngressFenced
	connecting.TransferBoundary = handoff.TransferBoundary{Kind: handoff.BoundaryProviderOrdered, Established: true}
	if result := r.AdvanceHandoff(context.Background(), connecting); result.Err != nil {
		t.Fatal(result.Err)
	}
	return connecting
}

func terminalLaunchTestRecord(handoffID string) terminalapp.LaunchRecord {
	return terminalapp.LaunchRecord{
		SchemaVersion:           terminalapp.LaunchRecordSchemaVersion,
		HandoffID:               handoffID,
		Provider:                core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"},
		AttachTargetFingerprint: strings.Repeat("a", 64),
		AttemptID:               strings.Repeat("b", 64),
		State:                   core.LaunchLaunching,
	}
}
