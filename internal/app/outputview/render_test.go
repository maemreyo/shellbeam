package outputview

import (
	"context"
	"strings"
	"testing"
)

func TestPreviewRendersHeadTailWithoutChangingRawRanges(t *testing.T) {
	store := retainedStore([]byte("abcdefghij"))
	result, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview, HeadBytes: 3, TailBytes: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "abc\n… 4 raw bytes omitted …\nhij" || len(result.Ranges) != 2 || result.Ranges[0] != (RawRange{Start: 0, End: 3}) || result.Ranges[1] != (RawRange{Start: 7, End: 10}) {
		t.Fatalf("result=%#v", result)
	}
}

func TestRenderedPreviewStripsANSIAndCollapsesCarriageReturnProgress(t *testing.T) {
	store := retainedStore([]byte("\x1b[31mred\x1b[0m\rblue\n"))
	result, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview, HeadBytes: 64}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "blue\n" {
		t.Fatalf("text=%q", result.Text)
	}
}

func TestRenderedPreviewReplacesInvalidUTF8AndSummarizesBinary(t *testing.T) {
	invalid := retainedStore([]byte{'a', 0xff, 'b'})
	result, err := New(invalid).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview, HeadBytes: 8}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "a�b" {
		t.Fatalf("invalid text=%q", result.Text)
	}

	binary := retainedStore([]byte{'a', 0, 'b', 1})
	result, err = New(binary).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorPreview, HeadBytes: 8}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "binary output") || !strings.Contains(result.Text, "4 bytes") {
		t.Fatalf("binary text=%q", result.Text)
	}
}
