package codeintel

import (
	"context"
	"errors"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SourceRefState string

const (
	SourceRefCurrent     SourceRefState = "current"
	SourceRefExpired     SourceRefState = "source_ref_expired"
	SourceRefUnavailable SourceRefState = "source_ref_unavailable"

	CodeQueryBudgetExceeded  = "code_intelligence_query_budget_exceeded"
	CodeSourceChanged        = "code_intelligence_source_changed_during_query"
	CodePositionInvalid      = "code_intelligence_position_invalid"
	CodeSourceRefUnavailable = "source_ref_unavailable"
	CodeUnsupportedEncoding  = "unsupported_source_encoding"
)

type BoundSource struct {
	Ref   core.SourceRef `json:"ref"`
	Bytes []byte         `json:"-"`
}

type SourceBinder interface {
	Bind(context.Context, workspace.Workspace, string) (BoundSource, error)
	Resolve(core.SourceRefID) (BoundSource, SourceRefState)
}

type SourceRetention interface {
	Retain(core.SourceRef, []byte) (BoundSource, error)
	Resolve(core.SourceRefID) (BoundSource, SourceRefState)
}

type Error struct {
	Code      string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func Retryable(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Retryable
}

func newError(code string, retryable bool, cause error) error {
	return &Error{Code: code, Retryable: retryable, Cause: cause}
}
