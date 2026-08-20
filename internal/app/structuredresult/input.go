package structuredresult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const outputHashChunkBytes = 64 << 10

var (
	errInvalidRecordQuery   = errors.New("invalid structured record query")
	errRawOutputMismatch    = errors.New("raw output reference mismatch")
	errRawOutputUnavailable = errors.New("raw output unavailable")
)

type Binder struct{ store InputStore }

func NewInputBinder(store InputStore) *Binder { return &Binder{store: store} }

func (b *Binder) BindTerminalOutput(ctx context.Context, rec receipt.Receipt) (core.StructuredInputRef, error) {
	if b == nil || b.store == nil || rec.Validate() != nil || !rec.State.Terminal() || rec.OutputBytes < 0 {
		return core.StructuredInputRef{}, fmt.Errorf("invalid terminal output binding")
	}
	digest, err := b.hashRange(ctx, operation.SessionID(rec.SessionID), rec.OutputBytes)
	if err != nil {
		return core.StructuredInputRef{}, err
	}
	ref := core.RawOutputRef{SessionID: rec.SessionID, StartByte: 0, EndByte: rec.OutputBytes, SHA256: digest}
	if err := ref.Validate(); err != nil {
		return core.StructuredInputRef{}, err
	}
	if err := b.store.PutRawOutputRef(ctx, ref); err != nil {
		return core.StructuredInputRef{}, err
	}
	return core.RawInputRef(ref), nil
}

func (b *Binder) ReadInputRange(ctx context.Context, input core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	ref, ok := input.Raw()
	if !ok {
		return nil, errRawOutputMismatch
	}
	if b == nil || b.store == nil || ref.Validate() != nil || offset < 0 || max < 0 {
		return nil, errRawOutputMismatch
	}
	stored, err := b.store.GetRawOutputRef(ctx, ref.SessionID)
	if err != nil || stored != ref {
		return nil, errRawOutputMismatch
	}
	length := ref.EndByte - ref.StartByte
	if offset > length {
		return nil, errRawOutputMismatch
	}
	remaining := length - offset
	if remaining == 0 || max == 0 {
		return []byte{}, nil
	}
	if int64(max) > remaining {
		max = int(remaining)
	}
	cursor := ref.StartByte + offset
	data, next, err := b.store.ReadOutput(ctx, operation.SessionID(ref.SessionID), cursor, max)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRawOutputUnavailable, err)
	}
	if len(data) == 0 || len(data) > max || next != cursor+int64(len(data)) {
		return nil, errRawOutputUnavailable
	}
	return data, nil
}

func (b *Binder) hashRange(ctx context.Context, id operation.SessionID, end int64) (string, error) {
	h := sha256.New()
	for cursor := int64(0); cursor < end; {
		want := outputHashChunkBytes
		if remaining := end - cursor; int64(want) > remaining {
			want = int(remaining)
		}
		data, next, err := b.store.ReadOutput(ctx, id, cursor, want)
		if err != nil || len(data) == 0 || len(data) > want || next != cursor+int64(len(data)) {
			if err == nil {
				err = errRawOutputUnavailable
			}
			return "", fmt.Errorf("%w: %v", errRawOutputUnavailable, err)
		}
		_, _ = h.Write(data)
		cursor = next
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
