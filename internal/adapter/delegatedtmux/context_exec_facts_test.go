package delegatedtmux

import "testing"

func TestParseTmuxFactsCarriesEscapedPaneTTYAndCurrentCWD(t *testing.T) {
	raw := `$1|@1|%1|456|fish|/dev/ttys042|/tmp/work\|tree\ c\'d\\e|0||/tmp/tmux.sock|123|tmux\ 3.6a`
	facts, err := parseTmuxFacts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if facts.PaneTTY != "/dev/ttys042" || facts.CWD != `/tmp/work|tree c'd\e` {
		t.Fatalf("facts=%#v", facts)
	}
	obs := (&Provider{}).observationFromFacts(&controlClient{}, privateState{ProviderGeneration: "gen_ctx"}, facts)
	if obs.PaneTTY != facts.PaneTTY || obs.CWD != facts.CWD {
		t.Fatalf("observation=%#v", obs)
	}
}
