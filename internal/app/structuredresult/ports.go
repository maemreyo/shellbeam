// Package structuredresult coordinates deterministic structured execution projections.
package structuredresult

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const MaxListRecords = 256

type RecordQuery struct {
	Offset int
	Limit  int
}

func (q RecordQuery) Validate() error {
	if q.Offset < 0 || q.Limit < 1 || q.Limit > MaxListRecords {
		return errInvalidRecordQuery
	}
	return nil
}

type InputStore interface {
	ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error)
	PutRawOutputRef(context.Context, core.RawOutputRef) error
	GetRawOutputRef(context.Context, string) (core.RawOutputRef, error)
}

type InputBinder interface {
	BindTerminalOutput(context.Context, receipt.Receipt) (core.RawOutputRef, error)
	ReadOutputRange(context.Context, core.RawOutputRef, int64, int) ([]byte, error)
}

type Repository interface {
	PutDerivation(context.Context, core.Derivation) error
	GetDerivation(context.Context, string) (core.Derivation, error)
	PutRecords(context.Context, string, []core.Record) error
	ListRecords(context.Context, string, RecordQuery) ([]core.Record, error)
	CompactRecords(context.Context, string) error
}
