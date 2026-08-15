package outputview

import (
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type memoryStore struct {
	data   []byte
	extent Extent
	reads  []memoryRead
}

type memoryRead struct {
	cursor int64
	max    int
}

func retainedStore(data []byte) *memoryStore {
	return &memoryStore{data: append([]byte(nil), data...), extent: Extent{SessionID: "s", State: RetentionRetained, Bytes: int64(len(data))}}
}

func (m *memoryStore) OutputExtent(context.Context, operation.SessionID) (Extent, error) {
	return m.extent, nil
}
func (m *memoryStore) ReadOutput(_ context.Context, _ operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	m.reads = append(m.reads, memoryRead{cursor: cursor, max: max})
	if cursor < 0 || cursor > int64(len(m.data)) {
		return nil, int64(len(m.data)), errors.New("cursor_out_of_range")
	}
	end := min(int64(len(m.data)), cursor+int64(max))
	return append([]byte(nil), m.data[cursor:end]...), end, nil
}

func TestReadRawRangePreservesCanonicalOffsetsAndUTF8Boundary(t *testing.T) {
	store := retainedStore([]byte("a€b"))
	result, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, StartByte: 0, MaxBytes: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "a" || len(result.Ranges) != 1 || result.Ranges[0] != (RawRange{Start: 0, End: 1}) || !result.Truncated || result.FrozenCutBytes != 5 {
		t.Fatalf("result=%#v", result)
	}
	if len(store.reads) != 1 || store.reads[0].max > 6 {
		t.Fatalf("reads=%#v", store.reads)
	}
}

func TestReadRawRangeRejectsOutOfRangeAndRetentionLoss(t *testing.T) {
	store := retainedStore([]byte("abc"))
	_, err := New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, StartByte: 4, MaxBytes: 1}})
	if !errors.Is(err, failure.OutputOutOfRange) {
		t.Fatalf("out of range err=%v", err)
	}

	store.extent.State = RetentionCompacted
	_, err = New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, MaxBytes: 1}})
	if !errors.Is(err, failure.OutputCompacted) {
		t.Fatalf("compacted err=%v", err)
	}

	store.extent.State = RetentionUnavailable
	_, err = New(store).Read(context.Background(), Request{SessionID: "s", Selector: Selector{Kind: SelectorRawRange, MaxBytes: 1}})
	if !errors.Is(err, failure.OutputUnavailable) {
		t.Fatalf("unavailable err=%v", err)
	}
}
