package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

func TestReproStoreLostResponseReplayFreezesOriginalCapsuleAndEmitsOneEvent(t *testing.T) {
	r := openReproRepository(t, reproTestLimits())
	createdAt := time.Date(2026, 8, 15, 2, 0, 0, 0, time.UTC)
	first := reproCapsule(t, "repro-create-1", "repro_01K00000000000000000000001", strings.Repeat("a", 64), createdAt)
	fingerprint := reproRequestFingerprint(t, first.CreateID, first.Execution.OperationID)

	got, created, err := r.CreateRepro(context.Background(), fingerprint, first)
	if err != nil || !created || !reflect.DeepEqual(got, first) {
		t.Fatalf("first create got=%#v created=%v err=%v", got, created, err)
	}

	later := first
	later.ReproID = "repro_01K00000000000000000000002"
	later.CaptureCutDigest = strings.Repeat("b", 64)
	got, created, err = r.CreateRepro(context.Background(), fingerprint, later)
	if err != nil || created || !reflect.DeepEqual(got, first) {
		t.Fatalf("lost-response replay got=%#v created=%v err=%v", got, created, err)
	}

	byCreate, found, err := r.GetReproByCreateID(context.Background(), first.CreateID)
	if err != nil || !found || !reflect.DeepEqual(byCreate, first) {
		t.Fatalf("by create=%#v found=%v err=%v", byCreate, found, err)
	}
	byID, found, err := r.GetRepro(context.Background(), first.ReproID)
	if err != nil || !found || !reflect.DeepEqual(byID, first) {
		t.Fatalf("by id=%#v found=%v err=%v", byID, found, err)
	}
	if _, found, err := r.GetRepro(context.Background(), later.ReproID); err != nil || found {
		t.Fatalf("non-winning repro id found=%v err=%v", found, err)
	}

	high, err := r.ObservationHighWatermark(context.Background())
	if err != nil || high != 1 {
		t.Fatalf("high=%d err=%v", high, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].Kind != observation.EventReproRecorded || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
	if obligations[0].Correlation.OperationID != first.Execution.OperationID || obligations[0].Correlation.SessionID != first.Execution.SessionID || obligations[0].Correlation.RepositoryID != first.Source.RepositoryID || obligations[0].Correlation.WorkspaceID != first.Source.WorkspaceID {
		t.Fatalf("correlation=%#v", obligations[0].Correlation)
	}
}

func TestReproStoreSameCreateIDDifferentFingerprintConflictsWithoutMutation(t *testing.T) {
	r := openReproRepository(t, reproTestLimits())
	first := reproCapsule(t, "repro-create-conflict", "repro_01K00000000000000000000003", strings.Repeat("a", 64), time.Now().UTC())
	fingerprint := reproRequestFingerprint(t, first.CreateID, first.Execution.OperationID)
	if _, created, err := r.CreateRepro(context.Background(), fingerprint, first); err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, err)
	}
	conflicting := strings.Repeat("f", 64)
	candidate := first
	candidate.ReproID = "repro_01K00000000000000000000004"
	candidate.CaptureCutDigest = strings.Repeat("d", 64)
	if _, created, err := r.CreateRepro(context.Background(), conflicting, candidate); err == nil || created || !strings.Contains(err.Error(), "operation_metadata_conflict") {
		t.Fatalf("conflict created=%v err=%v", created, err)
	}
	got, found, err := r.GetReproByCreateID(context.Background(), first.CreateID)
	if err != nil || !found || !reflect.DeepEqual(got, first) {
		t.Fatalf("stored capsule changed=%#v found=%v err=%v", got, found, err)
	}
	if high, _ := r.ObservationHighWatermark(context.Background()); high != 1 {
		t.Fatalf("conflict emitted another event high=%d", high)
	}
}

func TestReproStoreConcurrentSameRequestConvergesToOneDurableWinner(t *testing.T) {
	r := openReproRepository(t, reproTestLimits())
	createdAt := time.Now().UTC()
	first := reproCapsule(t, "repro-create-race", "repro_01K00000000000000000000005", strings.Repeat("1", 64), createdAt)
	second := reproCapsule(t, "repro-create-race", "repro_01K00000000000000000000006", strings.Repeat("2", 64), createdAt.Add(time.Millisecond))
	fingerprint := reproRequestFingerprint(t, first.CreateID, first.Execution.OperationID)
	type result struct {
		capsule core.Capsule
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, candidate := range []core.Capsule{first, second} {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, created, err := r.CreateRepro(context.Background(), fingerprint, candidate)
			results <- result{capsule: got, created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var got []result
	for item := range results {
		got = append(got, item)
	}
	if len(got) != 2 || got[0].err != nil || got[1].err != nil || got[0].capsule.ReproID != got[1].capsule.ReproID || got[0].capsule.CaptureCutDigest != got[1].capsule.CaptureCutDigest {
		t.Fatalf("concurrent results=%#v", got)
	}
	createdCount := 0
	for _, item := range got {
		if item.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created winners=%d results=%#v", createdCount, got)
	}
	stored, found, err := r.GetReproByCreateID(context.Background(), first.CreateID)
	if err != nil || !found || stored.ReproID != got[0].capsule.ReproID {
		t.Fatalf("stored=%#v found=%v err=%v", stored, found, err)
	}
	if high, _ := r.ObservationHighWatermark(context.Background()); high != 1 {
		t.Fatalf("concurrent create emitted %d changes", high)
	}
}

func TestReproStoreRejectsDuplicateReproIDAcrossCreateIDs(t *testing.T) {
	r := openReproRepository(t, reproTestLimits())
	first := reproCapsule(t, "repro-create-id-a", "repro_01K00000000000000000000007", strings.Repeat("a", 64), time.Now().UTC())
	second := reproCapsule(t, "repro-create-id-b", first.ReproID, strings.Repeat("b", 64), time.Now().UTC().Add(time.Second))
	if _, created, err := r.CreateRepro(context.Background(), reproRequestFingerprint(t, first.CreateID, first.Execution.OperationID), first); err != nil || !created {
		t.Fatal(err)
	}
	if _, created, err := r.CreateRepro(context.Background(), reproRequestFingerprint(t, second.CreateID, second.Execution.OperationID), second); err == nil || created || !strings.Contains(err.Error(), "repro_id_conflict") {
		t.Fatalf("duplicate repro id created=%v err=%v", created, err)
	}
}

func TestReproRetentionEvictsWholeOldRecordsByAgeCountAndBytes(t *testing.T) {
	limits := reproTestLimits()
	limits.MaxReproCapsules = 2
	limits.MaxReproAge = time.Hour
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	r := openReproRepository(t, limits)
	r.now = func() time.Time { return now.Add(-2 * time.Hour) }
	old := reproCapsule(t, "repro-create-old", "repro_01K00000000000000000000008", strings.Repeat("8", 64), now.Add(-2*time.Hour))
	if _, _, err := r.CreateRepro(context.Background(), reproRequestFingerprint(t, old.CreateID, old.Execution.OperationID), old); err != nil {
		t.Fatal(err)
	}

	r.now = func() time.Time { return now }
	middle := reproCapsule(t, "repro-create-mid", "repro_01K00000000000000000000009", strings.Repeat("9", 64), now.Add(-30*time.Minute))
	newest := reproCapsule(t, "repro-create-new", "repro_01K0000000000000000000000A", strings.Repeat("a", 64), now)
	for _, capsule := range []core.Capsule{middle, newest} {
		if _, _, err := r.CreateRepro(context.Background(), reproRequestFingerprint(t, capsule.CreateID, capsule.Execution.OperationID), capsule); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := r.GetReproByCreateID(context.Background(), old.CreateID); err != nil || found {
		t.Fatalf("expired capsule found=%v err=%v", found, err)
	}
	entries, err := r.reproEntriesLocked()
	if err != nil || len(entries) != 2 {
		t.Fatalf("retained entries=%d err=%v", len(entries), err)
	}

	third := reproCapsule(t, "repro-create-third", "repro_01K0000000000000000000000B", strings.Repeat("b", 64), now.Add(time.Minute))
	if _, _, err := r.CreateRepro(context.Background(), reproRequestFingerprint(t, third.CreateID, third.Execution.OperationID), third); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := r.GetReproByCreateID(context.Background(), middle.CreateID); found {
		t.Fatal("oldest retained capsule survived count ceiling")
	}

	byteLimits := reproTestLimits()
	byteLimits.MaxReproCapsules = 8
	first := reproCapsule(t, "repro-create-byte-a", "repro_01K0000000000000000000000C", strings.Repeat("c", 64), now)
	fingerprint := reproRequestFingerprint(t, first.CreateID, first.Execution.OperationID)
	encoded, err := json.Marshal(reproCreateRecord{SchemaVersion: 1, RequestFingerprint: fingerprint, Capsule: first})
	if err != nil {
		t.Fatal(err)
	}
	byteLimits.MaxReproBytes = int64(len(encoded)+1)*2 - 1
	byteStore := openReproRepository(t, byteLimits)
	byteStore.now = func() time.Time { return now }
	if _, _, err := byteStore.CreateRepro(context.Background(), fingerprint, first); err != nil {
		t.Fatal(err)
	}
	second := reproCapsule(t, "repro-create-byte-b", "repro_01K0000000000000000000000D", strings.Repeat("d", 64), now.Add(time.Second))
	if _, _, err := byteStore.CreateRepro(context.Background(), reproRequestFingerprint(t, second.CreateID, second.Execution.OperationID), second); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := byteStore.GetReproByCreateID(context.Background(), first.CreateID); found {
		t.Fatal("byte ceiling did not evict oldest whole create record")
	}
	if _, found, err := byteStore.GetReproByCreateID(context.Background(), second.CreateID); err != nil || !found {
		t.Fatalf("new byte-bounded capsule found=%v err=%v", found, err)
	}
}

func TestReproPreparedObservationReconcilesOnlyWhenCanonicalCreateExists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	limits := reproTestLimits()
	r := openReproRepositoryAt(t, root, limits)
	capsule := reproCapsule(t, "repro-create-gap", "repro_01K0000000000000000000000E", strings.Repeat("e", 64), time.Now().UTC())
	fingerprint := reproRequestFingerprint(t, capsule.CreateID, capsule.Execution.OperationID)
	r.writer = failNthAtomicWriter("replace.rename", 1)
	if _, created, err := r.CreateRepro(context.Background(), fingerprint, capsule); err != nil || !created {
		t.Fatalf("canonical create failed created=%v err=%v", created, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationPrepared {
		t.Fatalf("before restart=%#v err=%v", obligations, err)
	}
	r = openReproRepositoryAt(t, root, limits)
	if err := r.AbandonUnresolved(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	obligations, err = r.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("after canonical recovery=%#v err=%v", obligations, err)
	}

	missingRoot := filepath.Join(t.TempDir(), "missing-state")
	missing := openReproRepositoryAt(t, missingRoot, limits)
	prepared, result := missing.PrepareObservation(context.Background(), observation.PrepareRequest{
		Kind:        observation.EventReproRecorded,
		Correlation: observation.Correlation{OperationID: capsule.Execution.OperationID, SessionID: capsule.Execution.SessionID},
		SubjectRef:  "repro:repro-create-missing:repro_01K0000000000000000000000F", Summary: "reproduction capsule recorded",
	})
	if result.Err != nil || prepared.Obligation.ChangeSeq == 0 {
		t.Fatalf("prepare missing seq=%d result=%#v", prepared.Obligation.ChangeSeq, result)
	}
	missing = openReproRepositoryAt(t, missingRoot, limits)
	if err := missing.AbandonUnresolved(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	obligations, err = missing.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 1 || obligations[0].State != observation.ObligationAborted {
		t.Fatalf("missing canonical recovery=%#v err=%v", obligations, err)
	}
}

func openReproRepository(t *testing.T, limits Limits) *Repository {
	t.Helper()
	return openReproRepositoryAt(t, filepath.Join(t.TempDir(), "state"), limits)
}

func openReproRepositoryAt(t *testing.T, root string, limits Limits) *Repository {
	t.Helper()
	r, err := Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func reproTestLimits() Limits {
	return Limits{
		MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024,
		MaxReproCapsules: 8, MaxReproBytes: 2 << 20, MaxReproAge: 7 * 24 * time.Hour,
	}
}

func reproRequestFingerprint(t *testing.T, createID, operationID string) string {
	t.Helper()
	fingerprint, err := (core.CreateRequest{
		CreateID: createID, OperationID: operationID,
		Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent},
	}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func reproCapsule(t *testing.T, createID, reproID, cut string, createdAt time.Time) core.Capsule {
	t.Helper()
	capsule := core.Capsule{
		SchemaVersion: core.SchemaVersion, CreateID: createID, ReproID: reproID, CreatedAt: createdAt, CaptureCutDigest: cut,
		Execution: core.ExecutionDescriptor{
			OperationID: "op-repro", SessionID: "session-repro", ReceiptDigest: strings.Repeat("b", 64),
			CommandSemanticsFingerprint: strings.Repeat("c", 64), ExecutionMode: "argv", Executable: "go",
			ResolvedArgv: []string{"go", "test", "./..."}, CommandDetails: core.CaptureExact,
		},
		Source: core.SourceDescriptor{
			RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000",
			WorkspaceGeneration: "gen_" + strings.Repeat("d", 64), Quality: core.CapturePartial,
		},
		Project:     core.ProjectDescriptor{Quality: core.CaptureUnknown},
		Environment: core.EnvironmentDescriptor{EnvironmentQuality: core.CaptureUnknown, ToolchainQuality: core.CaptureUnknown},
		Input:       core.InputDescriptor{AcceptedBytes: 0, DeliveredBytes: 0, Complete: true, ContentIdentity: core.CaptureUnavailable},
		Results: []core.ReferenceDescriptor{{
			RefID: "structured:abc", RecordKind: "structured_result", ProducerID: "go-test-json", ProducerVersion: 1,
			SchemaVersion: 1, Digest: strings.Repeat("e", 64), Summary: "test results", OriginalAvailability: core.AvailabilityTerminal,
		}},
		Capture: core.CaptureMatrix{
			Source: core.CapturePartial, Command: core.CaptureExact, Toolchain: core.CaptureUnknown, Environment: core.CaptureUnknown,
			FilesystemExternal: core.CaptureUnknown, NetworkDependencies: core.CaptureUnknown, ExternalServices: core.CaptureUnknown,
			TimeRandomness: core.CaptureUnknown, Input: core.CapturePartial, Results: core.CaptureComplete,
		},
	}
	if err := capsule.Validate(); err != nil {
		t.Fatalf("capsule fixture: %v", err)
	}
	return capsule
}
