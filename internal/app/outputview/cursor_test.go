package outputview

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func outputCursorKey(digit string) observation.CursorKeyMaterial {
	return observation.CursorKeyMaterial{StateRootEpoch: "epoch_" + strings.Repeat(digit, 32), Generation: "key_" + strings.Repeat(digit, 32), Secret: []byte(strings.Repeat(digit, 32))}
}

func TestOutputCursorRoundTripAndBinding(t *testing.T) {
	codec, err := NewCursorCodec(outputCursorKey("0"))
	if err != nil {
		t.Fatal(err)
	}
	sel := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "boom", CaseSensitive: true, MaxMatches: 2}
	state := CursorState{FrozenCutBytes: 99, Offset: 20, Line: 4, Progress: 1, Phase: "search"}
	token, err := codec.Encode("s", sel, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) > MaxContinuationBytes || !strings.HasPrefix(token, OutputCursorPrefix) {
		t.Fatalf("token=%q", token)
	}
	got, err := codec.Decode(token, "s", sel)
	if err != nil {
		t.Fatal(err)
	}
	if got != state {
		t.Fatalf("got=%#v want=%#v", got, state)
	}

	changed := sel
	changed.Pattern = "other"
	if _, err := codec.Decode(token, "s", changed); err == nil {
		t.Fatal("selector mismatch accepted")
	}
	if _, err := codec.Decode(token, "other", sel); err == nil {
		t.Fatal("session mismatch accepted")
	}
	if _, err := codec.Decode(token+"x", "s", sel); err == nil {
		t.Fatal("tampering accepted")
	}
}

func TestOutputCursorExpiresAcrossKeyGeneration(t *testing.T) {
	oldCodec, _ := NewCursorCodec(outputCursorKey("0"))
	newCodec, _ := NewCursorCodec(outputCursorKey("1"))
	sel := Selector{Kind: SelectorLines, StartLine: 2, MaxLines: 2}
	token, err := oldCodec.Encode("s", sel, CursorState{FrozenCutBytes: 10, Offset: 5, Line: 2, Phase: "lines"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newCodec.Decode(token, "s", sel); err == nil {
		t.Fatal("expired cursor accepted")
	}
}
