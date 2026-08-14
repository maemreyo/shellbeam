package gojson

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type memoryReader struct {
	data  []byte
	input app.InputContext
	delay time.Duration
}

func newMemoryReader(text string) (*memoryReader, core.RawOutputRef) {
	data := []byte(text)
	sum := sha256.Sum256(data)
	ref := core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	return &memoryReader{data: data, input: app.InputContext{OperationID: "op-1"}}, ref
}

func (r *memoryReader) ReadOutputRange(_ context.Context, ref core.RawOutputRef, offset int64, max int) ([]byte, error) {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	if offset < 0 || max < 0 || offset > int64(len(r.data)) {
		return nil, errors.New("bad read")
	}
	end := offset + int64(max)
	if end > int64(len(r.data)) {
		end = int64(len(r.data))
	}
	return append([]byte(nil), r.data[offset:end]...), nil
}
func (r *memoryReader) DescribeInput(context.Context, core.RawOutputRef) (app.InputContext, error) {
	return r.input, nil
}

func generousLimits() app.Limits { return limitsWith(100, 4096) }
func limitsWith(records, stringBytes int) app.Limits {
	return app.Limits{MaxBytes: 1 << 20, MaxRecords: records, MaxStringBytes: stringBytes, MaxDepth: 16, MaxDuration: time.Second}
}
