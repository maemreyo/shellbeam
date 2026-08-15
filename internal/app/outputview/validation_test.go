package outputview

import "testing"

func TestRequestValidateRequiresExactlyOneBoundedSelector(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		ok   bool
	}{
		{name: "raw", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, StartByte: 2, MaxBytes: 8}}, ok: true},
		{name: "tail bytes", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailBytes: 8}}, ok: true},
		{name: "tail lines", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailLines: 2}}, ok: true},
		{name: "lines", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, StartLine: 1, MaxLines: 2}}, ok: true},
		{name: "preview", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview, HeadBytes: 8, TailBytes: 8}}, ok: true},
		{name: "search", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "boom", MaxMatches: 2}}, ok: true},
		{name: "missing session", req: Request{Selector: Selector{Kind: SelectorRawRange, MaxBytes: 1}}},
		{name: "unknown kind", req: Request{SessionID: "s", Selector: Selector{Kind: "wat"}}},
		{name: "raw zero max", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange}}},
		{name: "raw conflicting fields", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, MaxBytes: 1, TailBytes: 1}}},
		{name: "tail conflicting modes", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailBytes: 1, TailLines: 1}}},
		{name: "line zero start", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, MaxLines: 1}}},
		{name: "preview empty", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview}}},
		{name: "search empty", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, MaxMatches: 1}}},
		{name: "search bad mode", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: "glob", Pattern: "x", MaxMatches: 1}}},
		{name: "too many bytes", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, MaxBytes: MaxReturnBytes + 1}}},
		{name: "too many lines", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, StartLine: 1, MaxLines: MaxLines + 1}}},
		{name: "too many matches", req: Request{SessionID: "s", Selector: Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "x", MaxMatches: MaxMatches + 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSelectorFingerprintStableAndSensitive(t *testing.T) {
	a := Selector{Kind: SelectorSearch, SearchMode: SearchLiteral, Pattern: "boom", CaseSensitive: true, MaxMatches: 3}
	b := a
	fa, err := a.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := b.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fa == "" || fa != fb {
		t.Fatalf("fingerprints %q %q", fa, fb)
	}
	b.Pattern = "other"
	fc, err := b.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fc == fa {
		t.Fatal("selector change did not change fingerprint")
	}
}
