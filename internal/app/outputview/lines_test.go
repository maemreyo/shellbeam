package outputview

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestReadByteAndLineTails(t *testing.T) {
	store := retainedStore([]byte("one\ntwo\nthree\n"))
	bytesResult, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailBytes: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if bytesResult.Text != "three\n" || len(bytesResult.Ranges) != 1 || bytesResult.Ranges[0] != (RawRange{Start: 8, End: 14}) {
		t.Fatalf("byte tail=%#v", bytesResult)
	}

	linesResult, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailLines: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if linesResult.Text != "two\nthree\n" || len(linesResult.Ranges) != 1 || linesResult.Ranges[0] != (RawRange{Start: 4, End: 14}) {
		t.Fatalf("line tail=%#v", linesResult)
	}
}

func TestReadLineRangeUsesOneBasedLinesAndBoundsWork(t *testing.T) {
	store := retainedStore([]byte("zero\none\ntwo\nthree\n"))
	result, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, StartLine: 2, MaxLines: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "one\ntwo\n" || len(result.Ranges) != 1 || result.Ranges[0] != (RawRange{Start: 5, End: 13}) {
		t.Fatalf("result=%#v", result)
	}
	work := 0
	for _, read := range store.reads {
		work += read.max
	}
	if work > MaxWorkBytes+4 {
		t.Fatalf("unbounded requested work=%d reads=%#v", work, store.reads)
	}
}

func TestReadLineRangeRejectsMissingStartLine(t *testing.T) {
	store := retainedStore([]byte("one\ntwo\n"))
	_, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, StartLine: 4, MaxLines: 1}})
	if !errors.Is(err, failure.OutputOutOfRange) {
		t.Fatalf("err=%v", err)
	}
}

func TestHugeLogicalLinesStayWithinReturnAndWorkBudgets(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, MaxWorkBytes+128)

	lineStore := retainedStore(data)
	lineResult, err := newCursorService(t, lineStore).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorLines, StartLine: 1, MaxLines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(lineResult.Text)) > MaxReturnBytes || !lineResult.Partial || !lineResult.Truncated {
		t.Fatalf("line result bytes=%d partial=%v truncated=%v", len([]byte(lineResult.Text)), lineResult.Partial, lineResult.Truncated)
	}
	for _, read := range lineStore.reads {
		if read.max > MaxWorkBytes {
			t.Fatalf("line read=%#v", read)
		}
	}

	tailStore := retainedStore(data)
	tailResult, err := newCursorService(t, tailStore).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorTail, TailLines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(tailResult.Text)) > MaxReturnBytes || !tailResult.Partial || !tailResult.Truncated {
		t.Fatalf("tail result bytes=%d partial=%v truncated=%v", len([]byte(tailResult.Text)), tailResult.Partial, tailResult.Truncated)
	}
	for _, read := range tailStore.reads {
		if read.max > MaxWorkBytes {
			t.Fatalf("tail read=%#v", read)
		}
	}
}

func TestLineRangeContinuationResumesAtFirstUnreturnedByte(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, MaxWorkBytes+128)
	store := retainedStore(data)
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorLines, StartLine: 1, MaxLines: 1}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuation == "" || len(first.Ranges) != 1 || first.Ranges[0] != (RawRange{Start: 0, End: int64(MaxReturnBytes)}) {
		t.Fatalf("first continuation=%t ranges=%v partial=%v", first.Continuation != "", first.Ranges, first.Partial)
	}
	second, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: first.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Ranges) != 1 || second.Ranges[0].Start != first.Ranges[0].End || second.Ranges[0].End-second.Ranges[0].Start > int64(MaxReturnBytes) {
		t.Fatalf("second ranges=%v first ranges=%v continuation=%t", second.Ranges, first.Ranges, second.Continuation != "")
	}
}

func TestTailLineContinuationRetrievesPrecedingPartWithoutGap(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, MaxWorkBytes+128)
	store := retainedStore(data)
	svc := newCursorService(t, store)
	sel := Selector{Kind: SelectorTail, TailLines: 1}
	first, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel})
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuation == "" || len(first.Ranges) != 1 || first.Ranges[0].End != int64(len(data)) {
		t.Fatalf("first continuation=%t ranges=%v partial=%v", first.Continuation != "", first.Ranges, first.Partial)
	}
	second, err := svc.Read(context.Background(), Request{SessionID: "s", Selector: sel, Continuation: first.Continuation})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Ranges) != 1 || second.Ranges[0].End != first.Ranges[0].Start || second.Ranges[0].End-second.Ranges[0].Start > int64(MaxReturnBytes) {
		t.Fatalf("second ranges=%v first ranges=%v continuation=%t", second.Ranges, first.Ranges, second.Continuation != "")
	}
}
