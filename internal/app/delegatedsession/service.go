package delegatedsession

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
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

func (s *Service) humanProvider() (HumanProvider, error) {
	if s == nil || s.provider == nil {
		return nil, humanProviderUnavailable()
	}
	provider, ok := s.provider.(HumanProvider)
	if !ok {
		return nil, humanProviderUnavailable()
	}
	return provider, nil
}

func (s *Service) AttachHuman(ctx context.Context, ref core.ProviderRef, spec HumanAttachSpec) (HumanAttachResult, error) {
	provider, err := s.humanProvider()
	if err != nil {
		return HumanAttachResult{}, err
	}
	return provider.AttachHuman(ctx, ref, spec)
}

func (s *Service) SetHumanWritable(ctx context.Context, ref core.ProviderRef, client ProviderClientRef, writable bool) error {
	provider, err := s.humanProvider()
	if err != nil {
		return err
	}
	return provider.SetHumanWritable(ctx, ref, client, writable)
}

func (s *Service) FenceHumanIngress(ctx context.Context, ref core.ProviderRef, client ProviderClientRef, epoch core.AuthorityEpoch) (IngressFenceProof, error) {
	provider, err := s.humanProvider()
	if err != nil {
		return IngressFenceProof{}, err
	}
	return provider.FenceHumanIngress(ctx, ref, client, epoch)
}

func (s *Service) InspectHumanClient(ctx context.Context, ref core.ProviderRef, client ProviderClientRef) (HumanClientObservation, error) {
	provider, err := s.humanProvider()
	if err != nil {
		return HumanClientObservation{}, err
	}
	return provider.InspectHumanClient(ctx, ref, client)
}

func (s *Service) ArmWritableHumanControl(ctx context.Context, ref core.ProviderRef, client ProviderClientRef, spec HumanControlSpec) error {
	provider, err := s.humanProvider()
	if err != nil {
		return err
	}
	return provider.ArmWritableHumanControl(ctx, ref, client, spec)
}

func (s *Service) WaitWritableHumanControl(ctx context.Context, ref core.ProviderRef, client ProviderClientRef, spec HumanControlSpec) (handoff.HumanControlKind, error) {
	provider, err := s.humanProvider()
	if err != nil {
		return "", err
	}
	return provider.WaitWritableHumanControl(ctx, ref, client, spec)
}

func (s *Service) PrepareReadOnlyLocalControl(ctx context.Context, ref core.ProviderRef, client ProviderClientRef) error {
	provider, err := s.humanProvider()
	if err != nil {
		return err
	}
	return provider.PrepareReadOnlyLocalControl(ctx, ref, client)
}

func humanProviderUnavailable() error {
	return failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "provider_unconfigured"}, nil)
}

func (s *Service) privacyProvider() (PrivacyProvider, error) {
	if s == nil || s.provider == nil {
		return nil, privacyProviderUnavailable()
	}
	provider, ok := s.provider.(PrivacyProvider)
	if !ok {
		return nil, privacyProviderUnavailable()
	}
	return provider, nil
}

func (s *Service) ArmPrivateObservation(ctx context.Context, ref core.ProviderRef, spec PrivacySpec) (PrivacyHandle, error) {
	provider, err := s.privacyProvider()
	if err != nil {
		return PrivacyHandle{}, err
	}
	if err := spec.Validate(); err != nil {
		return PrivacyHandle{}, failure.New(failure.InvalidInput, map[string]string{"field": "privacy_spec"}, err)
	}
	return provider.ArmPrivateObservation(ctx, ref, spec)
}

func (s *Service) ProvePrivateObservation(ctx context.Context, ref core.ProviderRef, handle PrivacyHandle) (PrivateObservationProof, error) {
	provider, err := s.privacyProvider()
	if err != nil {
		return PrivateObservationProof{}, err
	}
	if err := handle.Validate(); err != nil {
		return PrivateObservationProof{}, failure.New(failure.InvalidInput, map[string]string{"field": "privacy_handle"}, err)
	}
	return provider.ProvePrivateObservation(ctx, ref, handle)
}

func (s *Service) ReleasePrivateObservation(ctx context.Context, ref core.ProviderRef, handle PrivacyHandle, boundary ForwardBoundary) error {
	provider, err := s.privacyProvider()
	if err != nil {
		return err
	}
	return provider.ReleasePrivateObservation(ctx, ref, handle, boundary)
}

func privacyProviderUnavailable() error {
	return failure.New(failure.PrivateOutputBarrierFailed, map[string]string{"reason": "provider_unconfigured"}, nil)
}
