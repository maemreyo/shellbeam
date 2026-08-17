package inputtrace

import (
	"context"
	"errors"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type Service struct {
	preparer Preparer
}

func New(preparer Preparer) *Service {
	return &Service{preparer: preparer}
}

func (s *Service) Prepare(ctx context.Context, request PrepareRequest) (Preparation, error) {
	mode, err := core.NormalizeMode(request.Mode)
	if err != nil {
		return Preparation{}, failure.New(failure.InvalidInput, map[string]string{"field": "trace_mode"}, err)
	}
	if mode == core.ModeOff {
		return Preparation{}, nil
	}
	if err := ctx.Err(); err != nil {
		return Preparation{}, err
	}
	if s == nil || s.preparer == nil {
		return Preparation{}, unavailable("not_configured", nil)
	}
	prepared, err := s.preparer.Prepare(ctx, request)
	if err != nil {
		if isTraceFailure(err) {
			return Preparation{}, err
		}
		return Preparation{}, unavailable("prepare_failed", err)
	}
	if prepared == nil {
		return Preparation{}, unavailable("empty_preparation", nil)
	}
	binding := prepared.Binding()
	if err := binding.Validate(); err != nil || binding.Mode != mode {
		_ = prepared.Abort()
		return Preparation{}, unavailable("invalid_binding", err)
	}
	environment := append([]operation.EnvironmentEntry(nil), prepared.EnvironmentAdditions()...)
	return Preparation{Handle: prepared, Binding: &binding, EnvironmentAdditions: environment}, nil
}

func unavailable(reason string, cause error) error {
	return failure.New(failure.InputTraceProviderUnavailable, map[string]string{"reason": reason}, cause)
}

func isTraceFailure(err error) bool {
	for _, code := range []failure.Code{
		failure.InputTraceProviderUnavailable,
		failure.InputTraceRequiredUnavailable,
		failure.InputTraceStartupBudgetExceeded,
		failure.InputTraceUnsupported,
		failure.InputTracePartial,
		failure.InputTraceBudgetExceeded,
		failure.InputTraceLateAttach,
		failure.InputTraceOwnershipLost,
		failure.InputTraceNotFound,
	} {
		if errors.Is(err, code) {
			return true
		}
	}
	return false
}
