package verification

import "testing"

func TestDerivedIDsAreStableAndSemantic(t *testing.T) {
	g := testGeneration()
	d1, err := DomainID(DomainSourceSelection, nil, g, []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := DomainID(DomainSourceSelection, nil, g, []string{"a", "b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 || len(d1) != 4+64 || d1[:4] != "dom_" {
		t.Fatalf("domain ids %q %q", d1, d2)
	}
	o1, err := ObligationID("pol_"+repeatHex('a'), "rule", g, []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	o2, err := ObligationID("pol_"+repeatHex('a'), "rule", g, []string{"y", "x", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if o1 != o2 {
		t.Fatal("trigger order changed obligation")
	}
	gap, err := PolicyGapID("pol_"+repeatHex('a'), "class", g, []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	if gap[:4] != "gap_" {
		t.Fatalf("gap=%q", gap)
	}
}
func repeatHex(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestRetryIDValidation(t *testing.T) {
	if err := ValidateActivationID("act_retry-1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWaiverID("wv_retry-1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateActivationID("bad"); err == nil {
		t.Fatal("bad activation accepted")
	}
	if err := ValidateWaiverID("wv_"); err == nil {
		t.Fatal("empty waiver accepted")
	}
}
