//go:build !darwin

package dyld

import (
	"context"
	"path/filepath"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

type HealthStatus struct {
	Available bool
	Reason    string
	Provider  trace.ProviderIdentity
	Coverage  trace.CoverageMatrix
}
type Provider struct{ stateDir string }

func New(stateDir string) *Provider { return &Provider{stateDir: filepath.Clean(stateDir)} }
func (p *Provider) Health(context.Context) (HealthStatus, error) {
	return HealthStatus{Reason: "unsupported_platform", Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Coverage: providerCoverage()}, nil
}
func (p *Provider) Prepare(_ context.Context, request traceapp.PrepareRequest) (traceapp.Prepared, error) {
	if request.Mode == trace.ModeRequired {
		return nil, failure.New(failure.InputTraceRequiredUnavailable, map[string]string{"provider": "dyld-interpose", "reason": "unsupported_platform"}, nil)
	}
	return nil, failure.New(failure.InputTraceProviderUnavailable, map[string]string{"provider": "dyld-interpose", "reason": "unsupported_platform"}, nil)
}
func (p *Provider) Finalize(context.Context, trace.InstrumentationBinding) (traceapp.ProviderSnapshot, error) {
	return traceapp.ProviderSnapshot{}, failure.New(failure.InputTraceNotFound, nil, nil)
}

var _ traceapp.Preparer = (*Provider)(nil)
var _ traceapp.Finalizer = (*Provider)(nil)
