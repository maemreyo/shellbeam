package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func setupSealedAdmissionExperiment(t *testing.T, root, experimentID string) (*Repository, *DecisionProtocolStore, dp.Episode) {
	t.Helper()
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	ep := dpStoredEpisode("ep-admission")
	ep.WorkspaceID = "ws_01M0CJX5KTQFA7JCHCRVC8SHFV"
	ep.Baseline.SourceGeneration = "gen_" + strings.Repeat("a", 64)
	if _, _, err := store.CreateEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	experiment := dpExperiment(experimentID, string(ep.EpisodeID))
	if _, _, err := store.DefineExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	digest, err := dp.PredictionSetDigest(nil)
	if err != nil {
		t.Fatal(err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seal := dp.ExperimentSeal{ExperimentID: experiment.ExperimentID, SourceGeneration: ep.Baseline.SourceGeneration, SealedPredictionDigest: digest, BaseProjectionCutRef: dp.DecisionProjectionCutRef{EpisodeID: ep.EpisodeID, CanonicalRecordHighWater: hw}, BaseCandidateProjectionDigest: "proj_" + strings.Repeat("b", 64), SealedAt: time.Unix(20, 0).UTC()}
	if _, _, err := store.SealExperimentCAS(ctx, seal); err != nil {
		t.Fatal(err)
	}
	return r, store, ep
}

func dpAdmissionReservation(opID string, obsFingerprint string) operation.Reservation {
	return operation.Reservation{SchemaVersion: 2, OperationID: operation.ID(opID), SessionID: operation.SessionID(opID + "-session"), WorkspaceID: "ws_01M0CJX5KTQFA7JCHCRVC8SHFV", RequestFingerprint: strings.Repeat("c", 64), ExecutionFingerprint: strings.Repeat("d", 64), ObservationBindingFingerprint: obsFingerprint, ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon", CreatedAt: time.Unix(30, 0).UTC()}
}

func withExperiment(r operation.Reservation, experimentID string) operation.Reservation {
	r.ExperimentID = experimentID
	return r
}

func experimentObservationFingerprint(t *testing.T, experimentID string) string {
	t.Helper()
	fp, err := (operation.ObservationBinding{ExperimentID: experimentID}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func TestReserveExperimentOperationDurablyCreatesReservationAndCanonicalLink(t *testing.T) {
	r, store, ep := setupSealedAdmissionExperiment(t, filepath.Join(t.TempDir(), "state"), "exp-admit")
	want := dpAdmissionReservation("op-admit", experimentObservationFingerprint(t, "exp-admit"))
	stored, _, created, result := r.ReserveExperimentOperation(context.Background(), withExperiment(want, "exp-admit"), dp.ExperimentExecutionLink{ExperimentID: "exp-admit"})
	if result.Err != nil || !created {
		t.Fatalf("created=%v err=%v", created, result.Err)
	}
	if stored.SessionID != want.SessionID {
		t.Fatalf("session=%s want=%s", stored.SessionID, want.SessionID)
	}
	hw, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	links := executionLinksFromStoreTest(t, records, "exp-admit")
	if len(links) != 1 {
		t.Fatalf("links=%#v", links)
	}
	link := links[0]
	if link.OperationID != "op-admit" || link.SessionID != string(want.SessionID) || link.WorkspaceID != ep.WorkspaceID || link.SourceGeneration != ep.Baseline.SourceGeneration || link.AcceptedRequestFingerprint != want.RequestFingerprint || link.AcceptedExecutionFingerprint != want.ExecutionFingerprint || link.AcceptedObservationBindingFingerprint != want.ObservationBindingFingerprint {
		t.Fatalf("link=%#v", link)
	}
}

func TestExperimentAdmissionReplayFingerprintMatrixRejectsOmittedChangedOrRemovedBeforeLiveLookup(t *testing.T) {
	r, _, _ := setupSealedAdmissionExperiment(t, filepath.Join(t.TempDir(), "state"), "exp-matrix")
	withE1 := dpAdmissionReservation("op-matrix", experimentObservationFingerprint(t, "exp-matrix"))
	if _, _, created, res := r.ReserveExperimentOperation(context.Background(), withExperiment(withE1, "exp-matrix"), dp.ExperimentExecutionLink{ExperimentID: "exp-matrix"}); res.Err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, res.Err)
	}
	for name, want := range map[string]operation.Reservation{"removed": dpAdmissionReservation("op-matrix", ""), "changed": dpAdmissionReservation("op-matrix", experimentObservationFingerprint(t, "exp-other"))} {
		t.Run(name, func(t *testing.T) {
			stored, created, res := r.ReserveOperation(context.Background(), want)
			_ = stored
			if created || res.Err == nil {
				t.Fatalf("created=%v err=%v", created, res.Err)
			}
			var ferr *failure.Failure
			if !errors.As(res.Err, &ferr) || ferr.Code != failure.OperationMetadataConflict {
				t.Fatalf("err=%v", res.Err)
			}
		})
	}
	if _, _, created, res := r.ReserveExperimentOperation(context.Background(), withExperiment(withE1, "exp-matrix"), dp.ExperimentExecutionLink{ExperimentID: "exp-matrix"}); res.Err != nil || created {
		t.Fatalf("same replay created=%v err=%v", created, res.Err)
	}
}

func TestExperimentAdmissionClaimPinsExperimentToFirstOperation(t *testing.T) {
	r, _, _ := setupSealedAdmissionExperiment(t, filepath.Join(t.TempDir(), "state"), "exp-pin")
	one := dpAdmissionReservation("op-one", experimentObservationFingerprint(t, "exp-pin"))
	if _, _, created, res := r.ReserveExperimentOperation(context.Background(), withExperiment(one, "exp-pin"), dp.ExperimentExecutionLink{ExperimentID: "exp-pin"}); res.Err != nil || !created {
		t.Fatal(res.Err)
	}
	two := dpAdmissionReservation("op-two", experimentObservationFingerprint(t, "exp-pin"))
	_, _, created, res := r.ReserveExperimentOperation(context.Background(), withExperiment(two, "exp-pin"), dp.ExperimentExecutionLink{ExperimentID: "exp-pin"})
	if res.Err == nil || created {
		t.Fatalf("second op created=%v err=%v", created, res.Err)
	}
	if reason, ok := dp.ReasonOf(res.Err); !ok || reason != dp.ReasonExperimentExecutionLimitReached {
		t.Fatalf("reason=%q err=%v", reason, res.Err)
	}
}

func TestExperimentAdmissionClaimDurableReservationAbsentReusesFrozenIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, _, _ := setupSealedAdmissionExperiment(t, root, "exp-claim-only")
	first := dpAdmissionReservation("op-claim", experimentObservationFingerprint(t, "exp-claim-only"))
	claim, err := ensureExperimentAdmissionClaimForTest(r, withExperiment(first, "exp-claim-only"), dp.ExperimentExecutionLink{ExperimentID: "exp-claim-only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(experimentOperationPathForTest(r, first.OperationID)); !os.IsNotExist(err) {
		t.Fatalf("reservation unexpectedly exists: %v", err)
	}
	r2 := openDecisionProtocolRepo(t, root)
	retry := first
	retry.SessionID = "new-session"
	retry.CreatedAt = time.Unix(99, 0).UTC()
	stored, _, created, res := r2.ReserveExperimentOperation(context.Background(), withExperiment(retry, "exp-claim-only"), dp.ExperimentExecutionLink{ExperimentID: "exp-claim-only"})
	if res.Err != nil || !created {
		t.Fatalf("created=%v err=%v", created, res.Err)
	}
	if string(stored.SessionID) != claim.SessionID || !stored.CreatedAt.Equal(claim.AdmittedAt) {
		t.Fatalf("stored=%#v claim=%#v", stored, claim)
	}
}

func TestExperimentAdmissionReservationDurableLinkAbsentRepairsAndAuthorizesSpawnOnce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep := setupSealedAdmissionExperiment(t, root, "exp-link-repair")
	want := dpAdmissionReservation("op-repair", experimentObservationFingerprint(t, "exp-link-repair"))
	claim, err := ensureExperimentAdmissionClaimForTest(r, withExperiment(want, "exp-link-repair"), dp.ExperimentExecutionLink{ExperimentID: "exp-link-repair"})
	if err != nil {
		t.Fatal(err)
	}
	frozenWant := reservationFromExperimentClaim(withExperiment(want, "exp-link-repair"), claim)
	if _, created, res := r.ReserveOperation(context.Background(), frozenWant); res.Err != nil || !created {
		t.Fatalf("raw reserve created=%v err=%v", created, res.Err)
	}
	hw, _ := store.CurrentHighWater(context.Background())
	records, _ := store.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	if len(executionLinksFromStoreTest(t, records, "exp-link-repair")) != 0 {
		t.Fatal("link unexpectedly exists")
	}
	r2 := openDecisionProtocolRepo(t, root)
	stored, _, created, res := r2.ReserveExperimentOperation(context.Background(), withExperiment(want, "exp-link-repair"), dp.ExperimentExecutionLink{ExperimentID: "exp-link-repair"})
	if res.Err != nil || !created {
		t.Fatalf("repair created=%v err=%v", created, res.Err)
	}
	if string(stored.SessionID) != claim.SessionID {
		t.Fatalf("stored session=%s claim=%s", stored.SessionID, claim.SessionID)
	}
	store2 := NewDecisionProtocolStore(r2)
	hw, _ = store2.CurrentHighWater(context.Background())
	records, _ = store2.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	links := executionLinksFromStoreTest(t, records, "exp-link-repair")
	if len(links) != 1 || links[0].LinkID != dp.LinkID(claim.LinkID) {
		t.Fatalf("links=%#v claim=%#v", links, claim)
	}
	if _, _, created, res := r2.ReserveExperimentOperation(context.Background(), withExperiment(want, "exp-link-repair"), dp.ExperimentExecutionLink{ExperimentID: "exp-link-repair"}); res.Err != nil || created {
		t.Fatalf("post-repair replay created=%v err=%v", created, res.Err)
	}
}

func executionLinksFromStoreTest(t *testing.T, records []dp.CanonicalRecordEnvelope, experimentID string) []dp.ExperimentExecutionLink {
	t.Helper()
	var out []dp.ExperimentExecutionLink
	for _, record := range records {
		if record.Kind != dp.RecordExperimentExecutionLink {
			continue
		}
		var link dp.ExperimentExecutionLink
		if err := json.Unmarshal(record.Body, &link); err != nil {
			t.Fatal(err)
		}
		if link.ExperimentID == dp.ExperimentID(experimentID) {
			out = append(out, link)
		}
	}
	return out
}

func ensureExperimentAdmissionClaimForTest(r *Repository, want operation.Reservation, requested dp.ExperimentExecutionLink) (experimentAdmissionClaim, error) {
	unlock := r.lock(want.OperationID)
	defer unlock()
	r.admit.Lock()
	defer r.admit.Unlock()
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return experimentAdmissionClaim{}, err
	}
	return r.ensureExperimentAdmissionClaimLocked(want, requested)
}

func experimentOperationPathForTest(r *Repository, id operation.ID) string {
	return filepath.Join(r.root, "operations", string(id)+".json")
}
