package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestExperimentAdmissionFaultClaimWriteFailureLeavesNoReservationOrLinkAndRetries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-fault-claim")
	want := withExperiment(dpAdmissionReservation("op-fault-claim", experimentObservationFingerprint(t, "exp-fault-claim")), "exp-fault-claim")
	failed := false
	r.writer.fail = func(point string) error {
		if !failed && point == "create.link" {
			failed = true
			return errors.New("claim write interrupted")
		}
		return nil
	}
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-claim"}); result.Err == nil || created {
		t.Fatalf("created=%v result=%#v", created, result)
	}
	if _, err := os.Stat(r.decisionProtocolExperimentAdmissionClaimPath("exp-fault-claim")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claim exists after failed first durable write: %v", err)
	}
	if _, err := os.Stat(experimentOperationPathForTest(r, want.OperationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reservation exists after claim failure: %v", err)
	}
	assertExperimentLinkCount(t, store, ep.EpisodeID, "exp-fault-claim", 0)
	r.writer.fail = nil
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-claim"}); result.Err != nil || !created {
		t.Fatalf("retry created=%v result=%#v", created, result)
	}
	assertExperimentLinkCount(t, store, ep.EpisodeID, "exp-fault-claim", 1)
}

func TestExperimentAdmissionFaultReservationDurableLinkInterruptedCompensatesThenRepairsPinnedClaim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-fault-link")
	want := withExperiment(dpAdmissionReservation("op-fault-link", experimentObservationFingerprint(t, "exp-fault-link")), "exp-fault-link")
	failed := false
	r.writer.fail = func(point string) error {
		if failed || point != "create.link" {
			return nil
		}
		_, opErr := os.Stat(experimentOperationPathForTest(r, want.OperationID))
		_, metadataErr := os.Stat(filepath.Join(root, "sessions", string(want.SessionID), "metadata.json"))
		if opErr == nil && metadataErr == nil {
			failed = true
			return errors.New("canonical link write interrupted")
		}
		return nil
	}
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-link"}); result.Err == nil || created {
		t.Fatalf("created=%v result=%#v", created, result)
	}
	if !failed {
		t.Fatal("fault never reached canonical link split")
	}
	claim, err := r.loadExperimentAdmissionClaimLocked("exp-fault-link")
	if err != nil {
		t.Fatal(err)
	}
	if claim.OperationID != string(want.OperationID) {
		t.Fatalf("claim=%#v", claim)
	}
	stored, err := r.LoadOperation(context.Background(), want.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := r.LoadSession(context.Background(), stored.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.State.Terminal() || snap.State != session.Abandoned {
		t.Fatalf("session not terminally compensated: %#v", snap)
	}
	assertExperimentLinkCount(t, store, ep.EpisodeID, "exp-fault-link", 0)
	r.writer.fail = nil
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-link"}); result.Err != nil || created {
		t.Fatalf("terminal retry created=%v result=%#v", created, result)
	}
	assertExperimentLinkCount(t, store, ep.EpisodeID, "exp-fault-link", 1)
	other := withExperiment(dpAdmissionReservation("op-fault-link-other", experimentObservationFingerprint(t, "exp-fault-link")), "exp-fault-link")
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), other, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-link"}); result.Err == nil || created {
		t.Fatalf("pinned claim reassigned created=%v result=%#v", created, result)
	}
}

func TestExperimentAdmissionFaultCanonicalLinkDurableHighWaterInterruptedReplaysOneLink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-fault-highwater")
	want := withExperiment(dpAdmissionReservation("op-fault-highwater", experimentObservationFingerprint(t, "exp-fault-highwater")), "exp-fault-highwater")
	before, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	r.writer.fail = func(point string) error {
		if failed || point != "replace.rename" {
			return nil
		}
		if _, err := os.Stat(r.decisionProtocolRecordPath(before + 1)); err == nil {
			failed = true
			return errors.New("link high-water interrupted")
		}
		return nil
	}
	_, _, _, first := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-highwater"})
	if first.Err == nil || !failed {
		t.Fatalf("result=%#v failed=%v", first, failed)
	}
	if _, err := os.Stat(r.decisionProtocolRecordPath(before + 1)); err != nil {
		t.Fatalf("canonical link record not durable: %v", err)
	}
	r.writer.fail = nil
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	if _, _, created, result := r2.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-highwater"}); result.Err != nil || created {
		t.Fatalf("retry created=%v result=%#v", created, result)
	}
	assertExperimentLinkCount(t, store2, ep.EpisodeID, "exp-fault-highwater", 1)
}

func TestExperimentAdmissionCorruptPrivateClaimDisagreesWithCanonicalLinkFailsClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-fault-disagree")
	want := withExperiment(dpAdmissionReservation("op-fault-disagree", experimentObservationFingerprint(t, "exp-fault-disagree")), "exp-fault-disagree")
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-disagree"}); result.Err != nil || !created {
		t.Fatalf("created=%v result=%#v", created, result)
	}
	claim, err := r.loadExperimentAdmissionClaimLocked("exp-fault-disagree")
	if err != nil {
		t.Fatal(err)
	}
	claim.LinkID = "link_" + strings.Repeat("d", 64)
	claim.LinkSemanticFingerprint = experimentLinkSemanticFingerprint(claim.executionLink())
	if result := atomicJSON(r.decisionProtocolExperimentAdmissionClaimPath("exp-fault-disagree"), claim); result.Err != nil {
		t.Fatal(result.Err)
	}
	if _, _, created, result := r.ReserveExperimentOperation(context.Background(), want, dp.ExperimentExecutionLink{ExperimentID: "exp-fault-disagree"}); result.Err == nil || created {
		t.Fatalf("corrupt replay created=%v result=%#v", created, result)
	}
	assertExperimentLinkCount(t, store, ep.EpisodeID, "exp-fault-disagree", 1)
}

func assertExperimentLinkCount(t *testing.T, store *DecisionProtocolStore, episodeID dp.EpisodeID, experimentID string, want int) {
	t.Helper()
	hw, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(context.Background(), episodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(executionLinksFromStoreTest(t, records, experimentID)); got != want {
		t.Fatalf("links=%d want=%d", got, want)
	}
}

func TestExperimentAdmissionConcurrentOrdinaryReserveAndInspectDoesNotDeadlock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-race-admission")
	experiment := withExperiment(dpAdmissionReservation("op-race-experiment", experimentObservationFingerprint(t, "exp-race-admission")), "exp-race-admission")
	ordinaryFingerprint, err := (operation.ObservationBinding{}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	ordinary := dpAdmissionReservation("op-race-ordinary", ordinaryFingerprint)
	ctx := context.Background()
	done := make(chan error, 3)
	go func() {
		_, _, _, result := r.ReserveExperimentOperation(ctx, experiment, dp.ExperimentExecutionLink{ExperimentID: "exp-race-admission"})
		done <- result.Err
	}()
	go func() {
		_, _, result := r.ReserveOperation(ctx, ordinary)
		done <- result.Err
	}()
	go func() {
		hw, err := store.CurrentHighWater(ctx)
		if err == nil {
			_, err = store.ListEpisodeRecords(ctx, ep.EpisodeID, hw)
		}
		done <- err
	}()
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
