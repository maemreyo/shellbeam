package daemon

import (
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type CachedEnvironmentBindingProvider interface {
	CachedEnvironmentBinding(operation.Reservation) (environment.Binding, bool)
}

func (s *Service) SetEnvironmentBindingProvider(provider CachedEnvironmentBindingProvider) {
	if s == nil {
		return
	}
	s.environmentBindings = provider
}

func (s *Service) freezeEnvironmentBinding(reservation *operation.Reservation) {
	if s == nil || reservation == nil || reservation.EnvironmentBinding != nil || s.environmentBindings == nil {
		return
	}
	binding, ok := s.environmentBindings.CachedEnvironmentBinding(*reservation)
	if !ok || binding.Validate() != nil {
		return
	}
	copy := binding
	reservation.EnvironmentBinding = &copy
}
