package capability

import "testing"

func qualifiedHermeticSupportForTest() HermeticBoundarySupport {
	return HermeticBoundarySupport{
		Version:            1,
		Maturity:           "experimental",
		Provider:           "bubblewrap",
		ProviderVersion:    "0.11.2",
		Scope:              "verification_only_ephemeral",
		Filesystem:         "immutable_capture",
		Network:            "off",
		Environment:        "fixed_allowlist",
		Stdin:              "closed",
		Writes:             "ephemeral_discard",
		TimeRandomness:     "ambient_nondeterministic",
		ChildTree:          "enclosed",
		Placement:          "pre_exec",
		PTY:                "unsupported",
		PersistentSessions: "unsupported",
		Authority:          "proven_input_scope",
	}
}

func TestHermeticBoundaryCapabilityIsSeparateAndFailClosed(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureHermeticBoundaryV1] != Unavailable || base.HermeticBoundary != nil {
		t.Fatalf("baseline overclaimed hermetic boundary: %#v", base.HermeticBoundary)
	}
	support := qualifiedHermeticSupportForTest()
	got := base.WithHermeticBoundary(support)
	if got.Features[FeatureHermeticBoundaryV1] != Available || got.HermeticBoundary == nil || *got.HermeticBoundary != support {
		t.Fatalf("hermetic support not advertised: %#v", got.HermeticBoundary)
	}
	clone := got.Clone()
	if clone.HermeticBoundary == got.HermeticBoundary || clone.HermeticBoundary == nil || *clone.HermeticBoundary != support {
		t.Fatal("hermetic capability clone aliased or changed")
	}
}

func TestHermeticBoundaryRejectsOverclaim(t *testing.T) {
	valid := qualifiedHermeticSupportForTest()
	cases := []HermeticBoundarySupport{
		{},
		func() HermeticBoundarySupport { x := valid; x.ProviderVersion = ""; return x }(),
		func() HermeticBoundarySupport { x := valid; x.Network = "allow"; return x }(),
		func() HermeticBoundarySupport { x := valid; x.PTY = "supported"; return x }(),
		func() HermeticBoundarySupport { x := valid; x.Authority = "probably_authoritative"; return x }(),
	}
	for _, support := range cases {
		got := Baseline(Limits{}).WithHermeticBoundary(support)
		if got.Features[FeatureHermeticBoundaryV1] != Unavailable || got.HermeticBoundary != nil {
			t.Fatalf("invalid support advertised: %#v", support)
		}
	}
}
