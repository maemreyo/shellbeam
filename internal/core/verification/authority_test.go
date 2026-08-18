package verification

import "testing"

func authorityGen(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return "gen_" + string(b)
}

func TestActivationIntentFingerprintExcludesServerFacts(t *testing.T) {
	intent := PolicyActivationIntent{ActivationID: "act_one", RepositoryID: "repo_01K00000000000000000000000", PreviousEffectiveDigest: "absent", ProposedPolicyDigest: "pol_" + repeatAuthority('a'), ProposalGeneration: authorityGen('1'), Authority: "explicit_caller", Actor: "tester"}
	a, err := ActivationIntentFingerprint(intent)
	if err != nil {
		t.Fatal(err)
	}
	commitA := PolicyActivationCommit{Intent: intent, ProposalOrigin: ProposalRepositoryAuthored, ActivationGeneration: authorityGen('2')}
	commitB := PolicyActivationCommit{Intent: intent, ProposalOrigin: ProposalGenerated, ProfileOrigin: "other", ActivationGeneration: authorityGen('3')}
	if err := commitA.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := commitB.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := ActivationIntentFingerprint(commitB.Intent)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("server commit facts changed intent fingerprint")
	}
	changed := intent
	changed.Actor = "other"
	c, err := ActivationIntentFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("caller-stable intent change did not change fingerprint")
	}
}

func TestRevocationIntentFingerprintIncludesRepositoryScope(t *testing.T) {
	a := WaiverRevocationIntent{RepositoryID: "repo_01K00000000000000000000000", WaiverID: "wv_same", Authority: "explicit_caller", Actor: "tester"}
	b := a
	b.RepositoryID = "repo_01K00000000000000000000001"
	fa, err := RevocationIntentFingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := RevocationIntentFingerprint(b)
	if err != nil {
		t.Fatal(err)
	}
	if fa == fb {
		t.Fatal("repository scope missing from revocation identity")
	}
}

func TestAuthorityIntentsRejectUnboundedAuditMetadata(t *testing.T) {
	intent := VerificationWaiverIntent{WaiverID: "wv_one", RepositoryID: "repo_01K00000000000000000000000", PolicyDigest: "pol_" + repeatAuthority('a'), RuleID: "r1", Phase: PhaseCheckpoint, Authority: "explicit_caller", Actor: "tester", Reason: "ok"}
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	intent.Reason = repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + repeatAuthority('x') + "x"
	if err := intent.Validate(); err == nil {
		t.Fatal("oversize waiver reason accepted")
	}
}
func repeatAuthority(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
