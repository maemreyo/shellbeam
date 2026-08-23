package store

import (
	"context"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSelectionCommitVsCloseUnresolvedRaceHasExactlyOneTerminalFact(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-terminal-race")
	intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-race")
	closure := unresolvedClosure(ep, commit.ProjectionDigest, "race")
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CommitSelectionCAS(context.Background(), intent, commit)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CloseEpisodeCAS(context.Background(), closure)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		if r, ok := dp.ReasonOf(err); ok && (r == dp.ReasonEpisodeTerminalConflict || r == dp.ReasonTerminalSelectionConflict) {
			conflict++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	hw, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for _, record := range records {
		if record.Kind == dp.RecordSelectionCommit || record.Kind == dp.RecordClosure {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal records=%d", terminal)
	}
}

func TestNormalVsOverrideSelectionRaceKeepsEpistemicCommitDistinct(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-normal-override-race")
	normalIntent, normal := selectionIntentAndCommit(t, ep, cand, "idem-normal-race")
	overrideIntent := normalIntent
	overrideIntent.Override = true
	overrideIntent.OverrideRef = "override-race"
	overrideFP, err := dp.SelectionIntentFingerprint(overrideIntent)
	if err != nil {
		t.Fatal(err)
	}
	if overrideFP == normal.SemanticIntentFingerprint {
		t.Fatal("override intent collapsed onto normal fingerprint")
	}
	override := normal
	override.CommitID = "commit-override-race"
	override.IdempotencyKey = "idem-override-race"
	override.OverrideRef = overrideIntent.OverrideRef
	override.SemanticIntentFingerprint = overrideFP
	override.OverrideAuthorization = &dp.OverrideAuthorization{AuthorityAttestationRef: "attest-race", AuthorityClass: storeAuthorityClass(), ActorRef: "trusted-user", Resolver: dp.ResolverRef{ProviderID: "trusted", ProviderVersion: "1", CapabilityVersion: "v1"}, ValidatedAt: time.Unix(60, 0).UTC(), QualificationCutDigest: "cut_" + strings.Repeat("c", 64)}
	if err := override.Validate(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CommitSelectionCAS(context.Background(), normalIntent, normal)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CommitSelectionCAS(context.Background(), overrideIntent, override)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		if r, ok := dp.ReasonOf(err); ok && r == dp.ReasonTerminalSelectionConflict {
			conflict++
			continue
		}
		t.Fatalf("unexpected error %v", err)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	hw, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, r := range records {
		if r.Kind == dp.RecordSelectionCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("selection commits=%d", commits)
	}
}
