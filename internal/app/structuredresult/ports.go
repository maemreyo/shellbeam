// Package structuredresult coordinates deterministic structured execution projections.
package structuredresult

import (
	"context"
	"errors"
	"path/filepath"
	"time"

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
	BindTerminalOutput(context.Context, receipt.Receipt) (core.StructuredInputRef, error)
	ReadInputRange(context.Context, core.StructuredInputRef, int64, int) ([]byte, error)
}

type Repository interface {
	PutDerivation(context.Context, core.Derivation) error
	GetDerivation(context.Context, string) (core.Derivation, error)
	PutRecords(context.Context, string, []core.Record) error
	ListRecords(context.Context, string, RecordQuery) ([]core.Record, error)
	CompactRecords(context.Context, string) error
}

type InputContext struct {
	OperationID     string
	RepositoryRoot  string
	DependencyRoots []string
	ToolchainRoots  []string
}

type Reader interface {
	ReadInputRange(context.Context, core.StructuredInputRef, int64, int) ([]byte, error)
	DescribeInput(context.Context, core.StructuredInputRef) (InputContext, error)
}

type Limits struct {
	MaxBytes       int64
	MaxRecords     int
	MaxStringBytes int
	MaxDepth       int
	MaxDuration    time.Duration
}

type ParseSummary struct {
	Records     int
	Errors      int
	Warnings    int
	TestPassed  int
	TestFailed  int
	TestSkipped int
}

type ParseResult struct {
	Records      []core.Record
	Outcome      core.ParseOutcome
	Completeness core.Completeness
	Summary      ParseSummary
}

type Adapter interface {
	ID() string
	Version() int
	Parse(context.Context, core.StructuredInputRef, Reader, Limits) (ParseResult, error)
}

func (c InputContext) Validate() error {
	if _, err := operation.ParseID(c.OperationID); err != nil {
		return err
	}
	if !validRoot(c.RepositoryRoot) || len(c.DependencyRoots) > 8 || len(c.ToolchainRoots) > 8 {
		return errors.New("invalid structured input context")
	}
	for _, root := range append(append([]string(nil), c.DependencyRoots...), c.ToolchainRoots...) {
		if !validRoot(root) || root == "" {
			return errors.New("invalid structured input context")
		}
	}
	return nil
}

func (l Limits) Validate() error {
	if l.MaxBytes < 1 || l.MaxBytes > 64<<20 || l.MaxRecords < 1 || l.MaxRecords > 4096 || l.MaxStringBytes < 1 || l.MaxStringBytes > 1<<20 || l.MaxDepth < 1 || l.MaxDepth > 64 || l.MaxDuration <= 0 || l.MaxDuration > time.Minute {
		return errors.New("invalid structured parse limits")
	}
	return nil
}

func validRoot(root string) bool {
	return root == "" || (filepath.IsAbs(root) && filepath.Clean(root) == root)
}
