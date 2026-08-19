package delegatedsession

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type Service struct{ provider Provider }

func New(provider Provider) *Service { return &Service{provider: provider} }

func (s *Service) Start(ctx context.Context, sessionMode string, req CreateRequest) (CreateResult, bool, error) {
	if sessionMode != core.ModeDelegatedInteractive {
		return CreateResult{}, false, nil
	}
	if s == nil || s.provider == nil {
		return CreateResult{}, true, delegatedUnavailable()
	}
	if err := s.provider.Probe(ctx); err != nil {
		return CreateResult{}, true, err
	}
	result, err := s.provider.Create(ctx, req)
	return result, true, err
}

func (s *Service) Reattach(ctx context.Context, ref core.ProviderRef, sink OutputSink) (Observation, error) {
	return s.provider.Reattach(ctx, ref, sink)
}
func (s *Service) Write(ctx context.Context, ref core.ProviderRef, data []byte) error {
	return s.provider.Write(ctx, ref, data)
}
func (s *Service) Signal(ctx context.Context, ref core.ProviderRef, signal string) error {
	return s.provider.Signal(ctx, ref, signal)
}
func (s *Service) Inspect(ctx context.Context, ref core.ProviderRef) (Observation, error) {
	return s.provider.Inspect(ctx, ref)
}
func (s *Service) Wait(ctx context.Context, ref core.ProviderRef) (Observation, error) {
	return s.provider.Wait(ctx, ref)
}
func (s *Service) Close(ctx context.Context, ref core.ProviderRef) error {
	return s.provider.Close(ctx, ref)
}

func delegatedUnavailable() error {
	return failure.New(failure.DelegatedSessionUnavailable, map[string]string{"reason": "provider_unconfigured"}, nil)
}
