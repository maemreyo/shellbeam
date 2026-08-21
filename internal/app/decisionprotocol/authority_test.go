package decisionprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type fakeAuthorityResolver struct {
	materialized     core.MaterializedAuthority
	qualified        core.DecisionAuthorityQualification
	materializeErr   error
	qualifyErr       error
	materializeCalls int
	qualifyCalls     int
}

func (f *fakeAuthorityResolver) MaterializeDecisionAuthority(context.Context, MaterializeAuthorityRequest) (core.MaterializedAuthority, error) {
	f.materializeCalls++
	return f.materialized, f.materializeErr
}
func (f *fakeAuthorityResolver) QualifyDecisionAuthority(context.Context, QualifyAuthorityRequest) (core.DecisionAuthorityQualification, error) {
	f.qualifyCalls++
	return f.qualified, f.qualifyErr
}

func (f *fakeEpisodeLedger) PutAuthorityAttestation(_ context.Context, a core.DecisionAuthorityAttestation) (core.CanonicalRecordEnvelope, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordAuthorityAttestation {
			continue
		}
		var e core.DecisionAuthorityAttestation
		if err := json.Unmarshal(r.Body, &e); err != nil {
			return core.CanonicalRecordEnvelope{}, false, err
		}
		if e.AttestationID == a.AttestationID {
			if reflect.DeepEqual(e, a) {
				return r, false, nil
			}
			return core.CanonicalRecordEnvelope{}, false, errors.New("attestation conflict")
		}
	}
	env, err := f.append(core.RecordAuthorityAttestation, a)
	return env, err == nil, err
}
func (f *fakeEpisodeLedger) FindAuthorityAttestation(_ context.Context, id string) (core.DecisionAuthorityAttestation, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordAuthorityAttestation {
			continue
		}
		var a core.DecisionAuthorityAttestation
		if err := json.Unmarshal(r.Body, &a); err != nil {
			return a, false, err
		}
		if a.AttestationID == id {
			return a, true, nil
		}
	}
	return core.DecisionAuthorityAttestation{}, false, nil
}
func (f *fakeEpisodeLedger) RecordOverride(_ context.Context, o core.DecisionOverride) (core.CanonicalRecordEnvelope, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordOverride {
			continue
		}
		var e core.DecisionOverride
		if err := json.Unmarshal(r.Body, &e); err != nil {
			return core.CanonicalRecordEnvelope{}, false, err
		}
		if e.OverrideID == o.OverrideID {
			if reflect.DeepEqual(e, o) {
				return r, false, nil
			}
			return core.CanonicalRecordEnvelope{}, false, errors.New("override conflict")
		}
	}
	env, err := f.append(core.RecordOverride, o)
	return env, err == nil, err
}
func (f *fakeEpisodeLedger) FindOverride(_ context.Context, id string) (core.DecisionOverride, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordOverride {
			continue
		}
		var o core.DecisionOverride
		if err := json.Unmarshal(r.Body, &o); err != nil {
			return o, false, err
		}
		if o.OverrideID == id {
			return o, true, nil
		}
	}
	return core.DecisionOverride{}, false, nil
}
func (f *fakeEpisodeLedger) FindSelectionCommitByIdempotencyKey(_ context.Context, key string) (core.SelectionCommit, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordSelectionCommit {
			continue
		}
		var c core.SelectionCommit
		if err := json.Unmarshal(r.Body, &c); err != nil {
			return c, false, err
		}
		if c.IdempotencyKey == key {
			return c, true, nil
		}
	}
	return core.SelectionCommit{}, false, nil
}

func authorityClassOwner() core.AuthorityClass {
	return core.AuthorityClass{Domain: "repo", ClassID: "repository_owner", Version: 1}
}
func authorityScope(ep core.EpisodeID) core.AuthorityScope {
	return core.AuthorityScope{RepositoryID: dpRepoID, EpisodeID: ep, ActionKind: core.AuthorityActionCommitSelectionOverride}
}
func trustedMaterialized(ep core.EpisodeID, status core.QualificationStatus) core.MaterializedAuthority {
	return core.MaterializedAuthority{Status: status, ActorRef: "trusted-user", AuthorityClass: authorityClassOwner(), Scope: authorityScope(ep), Resolver: core.ResolverRef{ProviderID: "trusted", ProviderVersion: "1", CapabilityVersion: "v1"}, ValidatedAt: time.Unix(50, 0).UTC(), QualificationCutDigest: "cut_" + strings.Repeat("a", 64), ProvenanceRef: "provider:trusted"}
}

func authorityService(t *testing.T, resolver AuthorityResolver) (*Service, *fakeEpisodeLedger, *fakePolicyStore) {
	t.Helper()
	content := task7Policy(core.DecisionRequirement{RequirementID: "challenge", Kind: core.RequirementCandidateChallenge, CandidateChallenge: &core.CandidateChallengeRequirement{MinimumDistinctLineages: 2}})
	content.OverridePolicy = core.OverridePolicy{Allowed: true, RequiredAuthorityClass: ptrAuthorityClass(authorityClassOwner())}
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	policy := &fakePolicyStore{put: true}
	policy.snapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: content}
	policy.currentSnapshot = policy.snapshot
	policy.currentActivation = core.PolicyActivation{ActivationID: "act-authority", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("b", 64), ActivationGeneration: "gen_" + strings.Repeat("c", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	policy.currentOK = true
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Selections: ledger, Authorities: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}, AuthorityResolver: resolver})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-authority", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCandidate(context.Background(), core.Candidate{CandidateID: "cand-a", EpisodeID: "ep-authority", SemanticClaim: "A", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(20, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	return svc, ledger, policy
}
func ptrAuthorityClass(v core.AuthorityClass) *core.AuthorityClass { return &v }

func TestAuthorityMaterializeRejectsCallerAuthoredAttestationBody(t *testing.T) {
	typ := reflect.TypeOf(MaterializeAuthorityRequest{})
	for _, forbidden := range []string{"Attestation", "AttestationID", "Resolver", "ProvenanceRef"} {
		if _, ok := typ.FieldByName(forbidden); ok {
			t.Fatalf("caller request exposes %s", forbidden)
		}
	}
}
func TestAuthorityMaterializePersistsOnlyProviderQualifiedAttestation(t *testing.T) {
	resolver := &fakeAuthorityResolver{materialized: trustedMaterialized("ep-authority", core.QualificationQualified)}
	svc, ledger, _ := authorityService(t, resolver)
	result, err := svc.MaterializeAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: "trusted-user", RequiredAuthorityClass: authorityClassOwner(), RequiredScope: authorityScope("ep-authority")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attestation == nil || result.Attestation.ActorRef != "trusted-user" || result.Attestation.Resolver.ProviderID != "trusted" {
		t.Fatalf("result=%#v", result)
	}
	count := 0
	for _, r := range ledger.records {
		if r.Kind == core.RecordAuthorityAttestation {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("attestations=%d", count)
	}
}
func TestAuthorityExactClassAndScopeMatchingHasNoImplicitLattice(t *testing.T) {
	bad := trustedMaterialized("ep-authority", core.QualificationQualified)
	bad.AuthorityClass = core.AuthorityClass{Domain: "repo", ClassID: "maintainer", Version: 1}
	resolver := &fakeAuthorityResolver{materialized: bad}
	svc, ledger, _ := authorityService(t, resolver)
	if _, err := svc.MaterializeAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: "trusted-user", RequiredAuthorityClass: authorityClassOwner(), RequiredScope: authorityScope("ep-authority")}); err == nil {
		t.Fatal("mismatched class materialized")
	}
	for _, r := range ledger.records {
		if r.Kind == core.RecordAuthorityAttestation {
			t.Fatal("unqualified attestation persisted")
		}
	}
}
func TestUnavailableOrUnknownAuthorityFailsClosed(t *testing.T) {
	for _, status := range []core.QualificationStatus{core.QualificationUnknown, core.QualificationUnavailable} {
		t.Run(string(status), func(t *testing.T) {
			m := trustedMaterialized("ep-authority", status)
			m.ActorRef = ""
			m.AuthorityClass = core.AuthorityClass{}
			m.Scope = core.AuthorityScope{}
			m.QualificationCutDigest = ""
			m.ProvenanceRef = ""
			resolver := &fakeAuthorityResolver{materialized: m}
			svc, ledger, _ := authorityService(t, resolver)
			result, err := svc.MaterializeAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: "trusted-user", RequiredAuthorityClass: authorityClassOwner(), RequiredScope: authorityScope("ep-authority")})
			if err != nil {
				t.Fatal(err)
			}
			if result.Attestation != nil || result.Status != status {
				t.Fatalf("result=%#v", result)
			}
			for _, r := range ledger.records {
				if r.Kind == core.RecordAuthorityAttestation {
					t.Fatal("failed qualification persisted")
				}
			}
		})
	}
}

func materializeTrustedAttestation(t *testing.T, svc *Service, resolver *fakeAuthorityResolver) core.DecisionAuthorityAttestation {
	t.Helper()
	resolver.materialized = trustedMaterialized("ep-authority", core.QualificationQualified)
	result, err := svc.MaterializeAuthority(context.Background(), MaterializeAuthorityRequest{ActorRef: "trusted-user", RequiredAuthorityClass: authorityClassOwner(), RequiredScope: authorityScope("ep-authority")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attestation == nil {
		t.Fatal("missing attestation")
	}
	return *result.Attestation
}
func qualifiedFor(att core.DecisionAuthorityAttestation, status core.QualificationStatus) core.DecisionAuthorityQualification {
	q := core.DecisionAuthorityQualification{Status: status, AttestationID: att.AttestationID, Resolver: core.ResolverRef{ProviderID: "trusted", ProviderVersion: "2", CapabilityVersion: "v1"}, ValidatedAt: time.Unix(70, 0).UTC()}
	if status == core.QualificationQualified {
		q.AuthorityClass = att.AuthorityClass
		q.ActorRef = att.ActorRef
		q.QualificationCutDigest = "cut_" + strings.Repeat("d", 64)
	}
	return q
}

func TestCreateOverrideUsesTrustedAttestedActorAndExactBlockerFreshness(t *testing.T) {
	resolver := &fakeAuthorityResolver{}
	svc, _, policy := authorityService(t, resolver)
	att := materializeTrustedAttestation(t, svc, resolver)
	p, err := svc.Project(context.Background(), "ep-authority", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol.Gate != core.GateBlocked {
		t.Fatalf("gate=%s", p.Protocol.Gate)
	}
	got, err := svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, BlockingRequirementDigest: p.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: att.AttestationID, Reason: "proceed despite challenge"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ActorRef != "trusted-user" || got.AuthorityAttestationRef != att.AttestationID || len(got.BlockingRequirements) == 0 {
		t.Fatalf("override=%#v", got)
	}
	_, err = svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, BlockingRequirementDigest: "block_" + strings.Repeat("e", 64), AuthorityAttestationRef: att.AttestationID, Reason: "stale"})
	if r, ok := core.ReasonOf(err); !ok || r != core.ReasonOverrideScopeStale {
		t.Fatalf("err=%v reason=%s", err, r)
	}
	_, err = svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: "proj_" + strings.Repeat("f", 64), BlockingRequirementDigest: p.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: att.AttestationID, Reason: "stale projection"})
	if r, ok := core.ReasonOf(err); !ok || r != core.ReasonProjectionConflict {
		t.Fatalf("err=%v reason=%s", err, r)
	}
}

func TestOverrideCommitRequalifiesAndFreezesAuthorizationCut(t *testing.T) {
	resolver := &fakeAuthorityResolver{}
	svc, _, policy := authorityService(t, resolver)
	att := materializeTrustedAttestation(t, svc, resolver)
	p, err := svc.Project(context.Background(), "ep-authority", "cand-a")
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, BlockingRequirementDigest: p.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: att.AttestationID, Reason: "authorized"})
	if err != nil {
		t.Fatal(err)
	}
	resolver.qualified = qualifiedFor(att, core.QualificationQualified)
	req := CommitSelectionRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ActorRef: "commit-caller", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, OverrideRef: ov.OverrideID, IdempotencyKey: "idem-override"}
	commit, err := svc.CommitSelection(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.qualifyCalls != 1 || commit.OverrideAuthorization == nil || commit.OverrideAuthorization.ActorRef != "trusted-user" || commit.OverrideAuthorization.Resolver.ProviderVersion != "2" {
		t.Fatalf("commit=%#v calls=%d", commit, resolver.qualifyCalls)
	}
	resolver.qualified = qualifiedFor(att, core.QualificationRevoked)
	replay, err := svc.CommitSelection(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.qualifyCalls != 1 || replay.CommitID != commit.CommitID {
		t.Fatalf("replay=%#v calls=%d", replay, resolver.qualifyCalls)
	}
}

func TestRevokedOrUnavailableAuthorityFailsClosedBeforeNewCommit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status core.QualificationStatus
		reason core.ReasonCode
	}{{"revoked", core.QualificationRevoked, core.ReasonOverrideAuthorityNotAdmissible}, {"unavailable", core.QualificationUnavailable, core.ReasonAuthorityRequirementUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeAuthorityResolver{}
			svc, _, policy := authorityService(t, resolver)
			att := materializeTrustedAttestation(t, svc, resolver)
			p, _ := svc.Project(context.Background(), "ep-authority", "cand-a")
			ov, err := svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, BlockingRequirementDigest: p.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: att.AttestationID, Reason: "try"})
			if err != nil {
				t.Fatal(err)
			}
			resolver.qualified = qualifiedFor(att, tc.status)
			_, err = svc.CommitSelection(context.Background(), CommitSelectionRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ActorRef: "caller", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, OverrideRef: ov.OverrideID, IdempotencyKey: "idem-" + tc.name})
			if r, ok := core.ReasonOf(err); !ok || r != tc.reason {
				t.Fatalf("err=%v reason=%s", err, r)
			}
		})
	}
}

func TestOverrideCommitPersistenceFailureRequalifiesWhenNoDurableCommit(t *testing.T) {
	resolver := &fakeAuthorityResolver{}
	svc, ledger, policy := authorityService(t, resolver)
	att := materializeTrustedAttestation(t, svc, resolver)
	p, _ := svc.Project(context.Background(), "ep-authority", "cand-a")
	ov, err := svc.CreateOverride(context.Background(), CreateOverrideRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, BlockingRequirementDigest: p.Protocol.BlockingRequirementDigest, AuthorityAttestationRef: att.AttestationID, Reason: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	resolver.qualified = qualifiedFor(att, core.QualificationQualified)
	ledger.selectionCommitFailures = 1
	req := CommitSelectionRequest{EpisodeID: "ep-authority", CandidateID: "cand-a", ActorRef: "caller", ExpectedPolicyDigest: policy.snapshot.PolicyDigest, ExpectedProjectionDigest: p.ProjectionDigest, OverrideRef: ov.OverrideID, IdempotencyKey: "idem-persist-fail"}
	if _, err := svc.CommitSelection(context.Background(), req); err == nil {
		t.Fatal("persistence failure not surfaced")
	}
	if resolver.qualifyCalls != 1 {
		t.Fatalf("calls=%d", resolver.qualifyCalls)
	}
	if _, err := svc.CommitSelection(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if resolver.qualifyCalls != 2 {
		t.Fatalf("retry did not requalify calls=%d", resolver.qualifyCalls)
	}
}
