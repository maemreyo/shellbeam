package decisionprotocol

import (
	"context"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func explicitCallerClass() core.AuthorityClass {
	return core.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
}

func explicitCallerScope() core.AuthorityScope {
	return core.AuthorityScope{RepositoryID: "repo_01K00000000000000000000000", EpisodeID: "episode-authority", ActionKind: core.AuthorityActionCommitSelectionOverride}
}

func TestBuiltInAuthorityProviderOnlyQualifiesShellBeamExplicitCaller(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	resolver := NewAuthorityResolverRegistry(func() time.Time { return now }, NewExplicitCallerAuthorityProvider(func() time.Time { return now }))
	actor := ExplicitCallerActorRef(501)
	got, err := resolver.MaterializeDecisionAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: actor, RequiredAuthorityClass: explicitCallerClass(), RequiredScope: explicitCallerScope()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.QualificationQualified || got.ActorRef != actor || !got.AuthorityClass.Equal(explicitCallerClass()) || got.Resolver.ProviderID != ExplicitCallerAuthorityProviderID {
		t.Fatalf("qualified=%#v", got)
	}
	if got.ProvenanceRef != "transport:unix-peer-uid:501" {
		t.Fatalf("provenance=%q", got.ProvenanceRef)
	}

	for _, class := range []core.AuthorityClass{
		{Domain: "repo", ClassID: "repository_owner", Version: 1},
		{Domain: "shellbeam", ClassID: "maintainer", Version: 1},
		{Domain: "shellbeam", ClassID: "*", Version: 1},
		{Domain: "*", ClassID: "explicit_caller", Version: 1},
	} {
		result, err := resolver.MaterializeDecisionAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: actor, RequiredAuthorityClass: class, RequiredScope: explicitCallerScope()})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == core.QualificationQualified {
			t.Fatalf("class %#v was promoted", class)
		}
	}
}

func TestBuiltInExplicitCallerRejectsForgedActorShape(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	resolver := NewAuthorityResolverRegistry(func() time.Time { return now }, NewExplicitCallerAuthorityProvider(func() time.Time { return now }))
	for _, actor := range []string{"caller-label", "shellbeam:explicit_caller:uid:", "shellbeam:explicit_caller:uid:abc", "shellbeam:explicit_caller:uid:-1", "shellbeam:explicit_caller:uid:501:extra"} {
		got, err := resolver.MaterializeDecisionAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: actor, RequiredAuthorityClass: explicitCallerClass(), RequiredScope: explicitCallerScope()})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == core.QualificationQualified {
			t.Fatalf("forged actor %q qualified", actor)
		}
	}
}

func TestAuthorityResolverRegistryDispatchesByExactClass(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	resolver := NewAuthorityResolverRegistry(func() time.Time { return now }, NewExplicitCallerAuthorityProvider(func() time.Time { return now }))
	actor := ExplicitCallerActorRef(42)
	q, err := resolver.QualifyDecisionAuthority(context.Background(), QualifyAuthorityRequest{AttestationID: "attestation-1", ExpectedActorRef: actor, RequiredAuthorityClass: explicitCallerClass(), RequiredScope: explicitCallerScope()})
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != core.QualificationQualified || q.ActorRef != actor || q.AttestationID != "attestation-1" {
		t.Fatalf("qualification=%#v", q)
	}
	unknown, err := resolver.QualifyDecisionAuthority(context.Background(), QualifyAuthorityRequest{AttestationID: "attestation-1", ExpectedActorRef: actor, RequiredAuthorityClass: core.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 2}, RequiredScope: explicitCallerScope()})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Status == core.QualificationQualified {
		t.Fatalf("wrong version qualified: %#v", unknown)
	}
}
