package outputview

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func newCursorService(t *testing.T, store Store) *Service {
	t.Helper()
	codec, err := NewCursorCodec(outputCursorKey("0"))
	if err != nil {
		t.Fatal(err)
	}
	return NewWithCursor(store, codec)
}

func TestSearchLiteralAndRegexReturnLineAndRawMatchRanges(t *testing.T) {
	store := retainedStore([]byte("alpha\nBoom here\nbeta boom\n"))
	svc := newCursorService(t, store)
	literal, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "boom", MaxMatches: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(literal.Matches) != 2 || literal.Matches[0].Line != 2 || literal.Matches[0].RawRange != (RawRange{Start: 6, End: 10}) || literal.Matches[1].Line != 3 || literal.Matches[1].RawRange != (RawRange{Start: 21, End: 25}) {
		t.Fatalf("literal=%#v", literal)
	}
	if literal.Continuation != "" || literal.Partial {
		t.Fatalf("literal continuation=%q partial=%v", literal.Continuation, literal.Partial)
	}

	regex, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchRegex, Pattern: `^beta`, CaseSensitive: true, MaxMatches: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(regex.Matches) != 1 || regex.Matches[0].RawRange != (RawRange{Start: 16, End: 20}) {
		t.Fatalf("regex=%#v", regex)
	}
}

func TestSearchRejectsMalformedRegex(t *testing.T) {
	svc := newCursorService(t, retainedStore([]byte("x\n")))
	_, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchRegex, Pattern: "(", MaxMatches: 1}})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchContinuationResumesWithoutDuplicateMatches(t *testing.T) {
	store := retainedStore([]byte("hit one\nmiss\nhit two\nhit three\n"))
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "hit", CaseSensitive: true, MaxMatches: 1}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Matches) != 1 || first.Matches[0].Line != 1 || first.Continuation == "" || !first.Partial {
		t.Fatalf("first=%#v", first)
	}
	second, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: first.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Matches) != 1 || second.Matches[0].Line != 3 || second.Continuation == "" {
		t.Fatalf("second=%#v", second)
	}
	third, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: second.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Matches) != 1 || third.Matches[0].Line != 4 || third.Continuation != "" || third.Partial {
		t.Fatalf("third=%#v", third)
	}
}

func TestContinuationKeepsFrozenCutWhenLiveOutputGrows(t *testing.T) {
	store := retainedStore([]byte("hit\nold\n"))
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "hit", CaseSensitive: true, MaxMatches: 1}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuation == "" {
		t.Fatalf("first=%#v", first)
	}
	store.data = append(store.data, []byte("hit new\n")...)
	store.extent.Bytes = int64(len(store.data))
	second, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: first.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if second.FrozenCutBytes != int64(len("hit\nold\n")) || len(second.Matches) != 0 || second.Continuation != "" {
		t.Fatalf("second=%#v", second)
	}
}

func TestSearchContinuationResumesWithinSameLine(t *testing.T) {
	store := retainedStore([]byte("hit hit\n"))
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "hit", CaseSensitive: true, MaxMatches: 1}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: first.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Matches) != 1 || len(second.Matches) != 1 || first.Matches[0].RawRange != (RawRange{Start: 0, End: 3}) || second.Matches[0].RawRange != (RawRange{Start: 4, End: 7}) || second.Continuation != "" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestSearchWorkBudgetReturnsContinuationForOversizedLine(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, MaxWorkBytes+64)
	store := retainedStore(data)
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "needle", CaseSensitive: true, MaxMatches: 2}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuation == "" || !first.Partial || len(first.Matches) != 0 {
		t.Fatalf("first=%#v", first)
	}
	for _, read := range store.reads {
		if read.max > MaxWorkBytes {
			t.Fatalf("read=%#v", read)
		}
	}
}
