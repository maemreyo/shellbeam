package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func storeAuthorityClass() dp.AuthorityClass {
	return dp.AuthorityClass{Domain: "repo", ClassID: "repository_owner", Version: 1}
}
func storeAuthorityScope(ep dp.EpisodeID, repo string) dp.AuthorityScope {
	return dp.AuthorityScope{RepositoryID: repo, EpisodeID: ep, ActionKind: dp.AuthorityActionCommitSelectionOverride}
}
func storeAttestation(ep dp.Episode) dp.DecisionAuthorityAttestation {
	return dp.DecisionAuthorityAttestation{SchemaVersion: 1, AttestationID: "attest-store", ActorRef: "trusted-user", AuthorityClass: storeAuthorityClass(), Scope: storeAuthorityScope(ep.EpisodeID, ep.RepositoryID), Resolver: dp.ResolverRef{ProviderID: "trusted", ProviderVersion: "1", CapabilityVersion: "v1"}, IssuedAt: time.Unix(50, 0).UTC(), ProvenanceRef: "provider:trusted"}
}

func TestAuthorityAttestationAndOverrideAreImmutableAndActorBound(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-authority-store")
	ctx := context.Background()
	att := storeAttestation(ep)
	first, created, err := store.PutAuthorityAttestation(ctx, att)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	replay, created, err := store.PutAuthorityAttestation(ctx, att)
	if err != nil || created || replay.CanonicalRecordSeq != first.CanonicalRecordSeq {
		t.Fatalf("replay=%#v created=%v err=%v", replay, created, err)
	}
	changed := att
	changed.AuthorityClass.ClassID = "maintainer"
	if _, _, err := store.PutAuthorityAttestation(ctx, changed); err == nil {
		t.Fatal("attestation identity rewrite accepted")
	}
	o := dp.DecisionOverride{OverrideID: "override-store", EpisodeID: ep.EpisodeID, CandidateID: cand.CandidateID, PolicyDigest: ep.PolicyBinding.PolicyDigest, ProjectionDigest: "proj_" + strings.Repeat("a", 64), BlockingRequirementDigest: "block_" + strings.Repeat("b", 64), BlockingRequirements: []string{"requirement:challenge:UNSATISFIED"}, ActorRef: att.ActorRef, AuthorityAttestationRef: att.AttestationID, Reason: "authorized", CreatedAt: time.Unix(51, 0).UTC()}
	if _, created, err := store.RecordOverride(ctx, o); err != nil || !created {
		t.Fatalf("override created=%v err=%v", created, err)
	}
	if _, created, err := store.RecordOverride(ctx, o); err != nil || created {
		t.Fatalf("override replay created=%v err=%v", created, err)
	}
	bad := o
	bad.OverrideID = "override-bad-actor"
	bad.ActorRef = "caller-label"
	if _, _, err := store.RecordOverride(ctx, bad); err == nil {
		t.Fatal("override accepted caller actor differing from trusted attestation")
	}
}

func TestAuthorityAttestationFindSurvivesRepositoryReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	_, store, ep, _ := selectionFixture(t, root, "ep-authority-reopen")
	att := storeAttestation(ep)
	if _, _, err := store.PutAuthorityAttestation(context.Background(), att); err != nil {
		t.Fatal(err)
	}
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	got, ok, err := store2.FindAuthorityAttestation(context.Background(), att.AttestationID)
	if err != nil || !ok || got.ActorRef != att.ActorRef || !got.AuthorityClass.Equal(att.AuthorityClass) {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}
