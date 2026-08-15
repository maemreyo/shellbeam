package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func a26EventObligations(t *testing.T, r *Repository) []observation.ObservationObligation {
	t.Helper()
	all, err := r.ListObservationObligations(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]observation.ObservationObligation, 0, len(all))
	for _, item := range all {
		if item.Kind == observation.EventMutationScopeChanged {
			out = append(out, item)
		}
	}
	return out
}

func TestMutationScopeSetCommitsOneSafeObservationAndExactRetryAddsNone(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-set", now, core.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatalf("set=%#v", got)
	}
	items := a26EventObligations(t, r)
	if len(items) != 1 {
		t.Fatalf("events=%#v", items)
	}
	got := items[0]
	if got.State != observation.ObligationCommitted || got.SubjectRef != "scope-a" || got.Summary != "set" || got.Correlation.ActivityID != "activity-a" || got.Correlation.WorkspaceID != string(mutationWorkspace) {
		t.Fatalf("event=%#v", got)
	}
	if strings.Contains(got.Summary, "src") || strings.Contains(got.Summary, "/") {
		t.Fatalf("unsafe summary=%q", got.Summary)
	}
	if replay := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); replay.Err != nil {
		t.Fatalf("replay=%#v", replay)
	}
	if items := a26EventObligations(t, r); len(items) != 1 {
		t.Fatalf("retry added event: %#v", items)
	}
}

func TestMutationScopeReplacementAndOldRetryProduceOnlyOneNewEvent(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	first, identity, firstReceipt := mutationScopeFixture("scope-a", "mutation-old", now, core.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), first, identity, firstReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	now = now.Add(time.Second)
	second, _, secondReceipt := mutationScopeFixture("scope-a", "mutation-new", now, core.DefaultTTL)
	second.Paths = []string{"src/auth/**"}
	secondReceipt.RequestFingerprint = strings.Repeat("b", 64)
	if got := r.CommitMutationScopeSet(context.Background(), second, identity, secondReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	if items := a26EventObligations(t, r); len(items) != 2 {
		t.Fatalf("events after replacement=%#v", items)
	}
	if got := r.CommitMutationScopeSet(context.Background(), first, identity, firstReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	if items := a26EventObligations(t, r); len(items) != 2 {
		t.Fatalf("old retry added event=%#v", items)
	}
}

func TestMutationScopeReleaseEventsOnlyForActiveStateChange(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, setReceipt := mutationScopeFixture("scope-a", "mutation-set", now, core.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, setReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	release := core.MutationReceipt{SchemaVersion: 1, MutationID: "mutation-release", RequestFingerprint: strings.Repeat("b", 64), Result: core.ResultReleased, ScopeID: "scope-a", CommittedAt: now.Add(time.Second)}
	if got := r.CommitMutationScopeRelease(context.Background(), "scope-a", release); got.Err != nil {
		t.Fatal(got.Err)
	}
	items := a26EventObligations(t, r)
	if len(items) != 2 || items[1].Summary != "released" || items[1].State != observation.ObligationCommitted {
		t.Fatalf("release events=%#v", items)
	}
	if got := r.CommitMutationScopeRelease(context.Background(), "scope-a", release); got.Err != nil {
		t.Fatal(got.Err)
	}
	if items := a26EventObligations(t, r); len(items) != 2 {
		t.Fatalf("release retry added event=%#v", items)
	}

	absent := core.MutationReceipt{SchemaVersion: 1, MutationID: "mutation-absent", RequestFingerprint: strings.Repeat("c", 64), Result: core.ResultReleased, ScopeID: "scope-a", CommittedAt: now.Add(2 * time.Second)}
	if got := r.CommitMutationScopeRelease(context.Background(), "scope-a", absent); got.Err != nil {
		t.Fatal(got.Err)
	}
	stored, found, err := r.LoadMutationReceipt(context.Background(), absent.MutationID)
	if err != nil || !found || stored.Result != core.ResultAlreadyAbsent {
		t.Fatalf("absent receipt=%#v found=%v err=%v", stored, found, err)
	}
	if items := a26EventObligations(t, r); len(items) != 2 {
		t.Fatalf("absent release added event=%#v", items)
	}
}

func TestMutationScopeExpiryEmitsNoTimerEvent(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-expire", "mutation-expire", now, core.MinTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	before := len(a26EventObligations(t, r))
	now = now.Add(core.MinTTL)
	if _, found, err := r.LoadMutationScope(context.Background(), s.ScopeID); err != nil || found {
		t.Fatalf("expired found=%v err=%v", found, err)
	}
	if after := len(a26EventObligations(t, r)); after != before {
		t.Fatalf("expiry added event before=%d after=%d", before, after)
	}
}

var _ = app.DurableChange

func TestMutationScopeObservationCommitFailureKeepsScopeTruthAndRecovers(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-recover", "mutation-recover", now, core.DefaultTTL)
	replaceWrites := 0
	r.writer.fail = func(point string) error {
		if point == "replace.write" {
			replaceWrites++
			if replaceWrites == 4 {
				return errors.New("inject observation commit write failure")
			}
		}
		return nil
	}
	result := r.CommitMutationScopeSet(context.Background(), s, identity, receipt)
	if result.Err != nil {
		t.Fatalf("canonical mutation failed: %#v replaceWrites=%d", result, replaceWrites)
	}
	active, found, err := r.LoadMutationScope(context.Background(), s.ScopeID)
	if err != nil || !found || active.RevisionID != receipt.MutationID {
		t.Fatalf("scope truth=%#v found=%v err=%v", active, found, err)
	}
	items := a26EventObligations(t, r)
	if len(items) != 1 || items[0].State != observation.ObligationPrepared {
		t.Fatalf("prepared event=%#v", items)
	}
	proofPath := r.mutationScopeObservationProofPath(uint64(items[0].ChangeSeq))
	if _, err := os.Stat(proofPath); err != nil {
		t.Fatalf("recovery proof missing: %v", err)
	}

	r.writer.fail = nil
	if err := r.reconcilePreparedExecutionObservations(context.Background()); err != nil {
		t.Fatal(err)
	}
	items = a26EventObligations(t, r)
	if len(items) != 1 || items[0].State != observation.ObligationCommitted {
		t.Fatalf("recovered event=%#v", items)
	}
	if _, err := os.Stat(proofPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("proof not cleaned: %v", err)
	}
}

func TestMutationScopeStartupReconcilesPendingBeforePreparedObservation(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-crash", "mutation-crash", now, core.DefaultTTL)
	createWrites := 0
	r.writer.fail = func(point string) error {
		if point == "create.write" {
			createWrites++
			if createWrites == 5 {
				return errors.New("inject proof write failure")
			}
		}
		return nil
	}
	first := r.CommitMutationScopeSet(context.Background(), s, identity, receipt)
	if first.Err == nil {
		t.Fatalf("proof fault did not surface: %#v createWrites=%d", first, createWrites)
	}
	items := a26EventObligations(t, r)
	if len(items) != 1 || items[0].State != observation.ObligationPrepared {
		t.Fatalf("before restart events=%#v", items)
	}
	if _, err := os.Stat(r.mutationScopePendingPath()); err != nil {
		t.Fatalf("pending missing: %v", err)
	}

	reopened, err := Open(root, r.limits)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now }
	if err := reopened.AbandonUnresolved(context.Background(), "incarnation-after-crash"); err != nil {
		t.Fatal(err)
	}
	active, found, err := reopened.LoadMutationScope(context.Background(), s.ScopeID)
	if err != nil || !found || active.RevisionID != receipt.MutationID {
		t.Fatalf("recovered scope=%#v found=%v err=%v", active, found, err)
	}
	items = a26EventObligations(t, reopened)
	if len(items) != 1 || items[0].State != observation.ObligationCommitted {
		t.Fatalf("after restart events=%#v", items)
	}
	if _, err := os.Stat(reopened.mutationScopePendingPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending not cleared: %v", err)
	}
}

func TestMutationScopePreparedReplacementDoesNotUseOldScopeAsRecoveryProof(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	old, identity, oldReceipt := mutationScopeFixture("scope-stale", "mutation-old", now, core.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), old, identity, oldReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	prepared, result := r.PrepareObservation(context.Background(), observation.PrepareRequest{
		Kind:        observation.EventMutationScopeChanged,
		Correlation: observation.Correlation{ActivityID: identity.ActivityID, WorkspaceID: string(identity.WorkspaceID)},
		SubjectRef:  old.ScopeID, Summary: "set",
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if prepared.Obligation.State != observation.ObligationPrepared {
		t.Fatalf("prepared=%#v", prepared)
	}
	if err := r.reconcilePreparedExecutionObservations(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, err := r.ListObservationObligations(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var replacement observation.ObservationObligation
	for _, item := range all {
		if item.ChangeSeq == prepared.Obligation.ChangeSeq {
			replacement = item
		}
	}
	if replacement.State != observation.ObligationAborted || replacement.AbortReason != observationAbortMissing {
		t.Fatalf("stale replacement recovery=%#v", replacement)
	}
	active, found, err := r.LoadMutationScope(context.Background(), old.ScopeID)
	if err != nil || !found || active.RevisionID != "mutation-old" {
		t.Fatalf("old scope truth changed=%#v found=%v err=%v", active, found, err)
	}
}
