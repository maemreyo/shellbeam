package structuredresult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type inputStoreFake struct {
	output    map[operation.SessionID][]byte
	refs      map[string]core.RawOutputRef
	available bool
	reads     int
}

func newInputStoreFake() *inputStoreFake {
	return &inputStoreFake{output: map[operation.SessionID][]byte{}, refs: map[string]core.RawOutputRef{}, available: true}
}

func (f *inputStoreFake) ReadOutput(_ context.Context, id operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	f.reads++
	if !f.available {
		return nil, cursor, errors.New("raw_output_unavailable")
	}
	data, ok := f.output[id]
	if !ok || cursor < 0 || cursor > int64(len(data)) {
		return nil, cursor, errors.New("raw_output_unavailable")
	}
	end := cursor + int64(max)
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return append([]byte(nil), data[cursor:end]...), end, nil
}

func (f *inputStoreFake) PutRawOutputRef(_ context.Context, ref core.RawOutputRef) error {
	if got, ok := f.refs[ref.SessionID]; ok && got != ref {
		return errors.New("raw_output_ref_conflict")
	}
	f.refs[ref.SessionID] = ref
	return nil
}

func (f *inputStoreFake) GetRawOutputRef(_ context.Context, sessionID string) (core.RawOutputRef, error) {
	ref, ok := f.refs[sessionID]
	if !ok {
		return core.RawOutputRef{}, errors.New("raw_output_ref_not_found")
	}
	return ref, nil
}

func TestOutputRefBinderPinsExactTerminalRange(t *testing.T) {
	store := newInputStoreFake()
	store.output["session-1"] = []byte("abcTAIL")
	binder := NewInputBinder(store)
	rec := terminalReceipt("session-1", 3)
	ref, err := binder.BindTerminalOutput(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("abc"))
	raw, ok := ref.Raw()
	if !ok || raw.StartByte != 0 || raw.EndByte != 3 || raw.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("ref=%#v", ref)
	}
	got, err := binder.ReadInputRange(context.Background(), ref, 1, 99)
	if err != nil || string(got) != "bc" {
		t.Fatalf("read=%q err=%v", got, err)
	}
	badRaw := raw
	badRaw.SHA256 = hex.EncodeToString(sha256.New().Sum(nil))
	bad := core.RawInputRef(badRaw)
	reads := store.reads
	if _, err := binder.ReadInputRange(context.Background(), bad, 0, 1); err == nil || store.reads != reads {
		t.Fatalf("mismatched ref read allowed err=%v reads=%d->%d", err, reads, store.reads)
	}
}

func TestOutputRefBinderSupportsEmptyOutputAndFailsClosedAfterCompaction(t *testing.T) {
	store := newInputStoreFake()
	store.output["empty-session"] = nil
	binder := NewInputBinder(store)
	ref, err := binder.BindTerminalOutput(context.Background(), terminalReceipt("empty-session", 0))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(nil)
	emptyRaw, ok := ref.Raw()
	if !ok || emptyRaw.EndByte != 0 || emptyRaw.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("empty ref=%#v", ref)
	}
	store.output["non-empty"] = []byte("x")
	nonEmpty, err := binder.BindTerminalOutput(context.Background(), terminalReceipt("non-empty", 1))
	if err != nil {
		t.Fatal(err)
	}
	store.available = false
	if _, err := binder.ReadInputRange(context.Background(), nonEmpty, 0, 1); err == nil {
		t.Fatal("compacted raw output remained readable")
	}
}

func TestOutputRefBinderRejectsNonTerminalAndShortCanonicalOutput(t *testing.T) {
	store := newInputStoreFake()
	store.output["session-1"] = []byte("ab")
	binder := NewInputBinder(store)
	rec := terminalReceipt("session-1", 3)
	if _, err := binder.BindTerminalOutput(context.Background(), rec); err == nil {
		t.Fatal("receipt range beyond canonical output accepted")
	}
	rec.State = session.Running
	rec.Outcome = session.NoOutcome
	if _, err := binder.BindTerminalOutput(context.Background(), rec); err == nil {
		t.Fatal("non-terminal receipt accepted")
	}
}

func terminalReceipt(sessionID string, outputBytes int64) receipt.Receipt {
	return receipt.Receipt{SchemaVersion: 1, OperationID: "op-1", SessionID: sessionID, DaemonIncarnation: "daemon", State: session.Failed, Outcome: session.Failure, OutputBytes: outputBytes, OutputComplete: true}
}
