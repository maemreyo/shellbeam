//go:build darwin

package dyld

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/oklog/ulid/v2"
)

type HealthStatus struct {
	Available bool
	Reason    string
	Provider  trace.ProviderIdentity
	Coverage  trace.CoverageMatrix
}

type Provider struct {
	stateDir         string
	clangPath        string
	source           string
	compilerIdentity func(context.Context) (string, error)
	compile          func(context.Context, string, string, string) error
	artifactMu       sync.Mutex
	mu               sync.Mutex
	active           map[string]*collector
	now              func() time.Time
}

func New(stateDir string) *Provider {
	p := &Provider{stateDir: filepath.Clean(stateDir), clangPath: "/usr/bin/clang", source: interposeSource, compile: defaultCompile, active: map[string]*collector{}, now: time.Now}
	p.compilerIdentity = func(ctx context.Context) (string, error) { return defaultCompilerIdentity(ctx, p.clangPath) }
	return p
}

func (p *Provider) Health(ctx context.Context) (HealthStatus, error) {
	status := HealthStatus{Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Coverage: providerCoverage()}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	if p == nil || p.stateDir == "" {
		status.Reason = "invalid_state"
		return status, nil
	}
	if err := validateExistingProviderLayout(p.stateDir); err != nil {
		status.Reason = "unsafe_state"
		return status, nil
	}
	info, err := os.Lstat(p.clangPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		status.Reason = "compiler_unavailable"
		return status, nil
	}
	status.Available = true
	return status, nil
}

func (p *Provider) Prepare(ctx context.Context, request traceapp.PrepareRequest) (traceapp.Prepared, error) {
	mode, err := trace.NormalizeMode(request.Mode)
	if err != nil || mode == trace.ModeOff {
		return nil, failure.New(failure.InputTraceUnsupported, map[string]string{"reason": "mode"}, err)
	}
	if mode == trace.ModeRequired {
		return nil, failure.New(failure.InputTraceRequiredUnavailable, map[string]string{"provider": "dyld-interpose", "platform": "darwin", "reason": "no_complete_pre_exec_coverage"}, nil)
	}
	health, err := p.Health(ctx)
	if err != nil {
		return nil, err
	}
	if !health.Available {
		return nil, failure.New(failure.InputTraceProviderUnavailable, map[string]string{"provider": "dyld-interpose", "platform": "darwin", "reason": health.Reason}, nil)
	}
	if err := ensureProviderLayout(p.stateDir); err != nil {
		return nil, p.unavailable("unsafe_state", err)
	}
	artifact, fingerprint, err := p.ensureArtifact(ctx)
	if err != nil {
		return nil, p.unavailable("compiler_failed", err)
	}
	traceID := "trace_" + ulid.Make().String()
	traceDir := filepath.Join(tracesRoot(p.stateDir), traceID)
	if err := ensurePrivateDir(traceDir); err != nil {
		return nil, p.unavailable("trace_state_failed", err)
	}
	socketDir, err := ensureSocketRoot()
	if err != nil {
		_ = os.Remove(traceDir)
		return nil, p.unavailable("socket_state_failed", err)
	}
	collector, err := newCollector(traceDir, socketDir, traceID, defaultCollectorLimits())
	if err != nil {
		_ = os.Remove(traceDir)
		return nil, p.unavailable("collector_failed", err)
	}
	binding := trace.InstrumentationBinding{SchemaVersion: trace.SchemaVersion, TraceID: traceID, Mode: trace.ModeBestEffort, Status: trace.BindingActive,
		Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin", InstrumentationFingerprint: fingerprint,
		InstrumentationEffect: trace.EffectEnvironmentAffecting, PreExecCoverageEstablished: false, Coverage: providerCoverage()}
	if err := binding.Validate(); err != nil {
		collector.abort()
		return nil, p.unavailable("binding_invalid", err)
	}
	p.mu.Lock()
	p.active[traceID] = collector
	p.mu.Unlock()
	environment := []operation.EnvironmentEntry{{Key: "DYLD_INSERT_LIBRARIES", Value: artifact}, {Key: "SHELLBEAM_TRACE_SOCKET", Value: collector.socketPath}, {Key: "SHELLBEAM_TRACE_PROTOCOL", Value: "1"}, {Key: "SHELLBEAM_TRACE_ID", Value: traceID}}
	return &preparedTrace{provider: p, traceID: traceID, binding: binding, environment: environment}, nil
}

func (p *Provider) Finalize(ctx context.Context, binding trace.InstrumentationBinding) (traceapp.ProviderSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return traceapp.ProviderSnapshot{}, err
	}
	if err := binding.Validate(); err != nil || binding.Provider.ID != "dyld-interpose" {
		return traceapp.ProviderSnapshot{}, failure.New(failure.InputTraceNotFound, map[string]string{"trace_id": binding.TraceID}, err)
	}
	p.mu.Lock()
	collector := p.active[binding.TraceID]
	delete(p.active, binding.TraceID)
	p.mu.Unlock()
	if collector == nil {
		return traceapp.ProviderSnapshot{}, failure.New(failure.InputTraceNotFound, map[string]string{"trace_id": binding.TraceID}, nil)
	}
	return collector.finalize(), nil
}

func (p *Provider) abort(traceID string) {
	p.mu.Lock()
	collector := p.active[traceID]
	delete(p.active, traceID)
	p.mu.Unlock()
	if collector != nil {
		collector.abort()
	}
}

func (p *Provider) activeCount() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.active) }
func (p *Provider) unavailable(reason string, cause error) error {
	return failure.New(failure.InputTraceProviderUnavailable, map[string]string{"provider": "dyld-interpose", "platform": "darwin", "reason": reason}, cause)
}

type preparedTrace struct {
	provider    *Provider
	traceID     string
	binding     trace.InstrumentationBinding
	environment []operation.EnvironmentEntry
	once        sync.Once
}

func (p *preparedTrace) Binding() trace.InstrumentationBinding { return p.binding }
func (p *preparedTrace) EnvironmentAdditions() []operation.EnvironmentEntry {
	return append([]operation.EnvironmentEntry(nil), p.environment...)
}
func (p *preparedTrace) Abort() error { p.once.Do(func() { p.provider.abort(p.traceID) }); return nil }

var _ traceapp.Preparer = (*Provider)(nil)
var _ traceapp.Finalizer = (*Provider)(nil)

func (p *Provider) Cleanup(ctx context.Context, binding trace.InstrumentationBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := binding.Validate(); err != nil || binding.Provider.ID != "dyld-interpose" {
		return failure.New(failure.InputTraceNotFound, map[string]string{"trace_id": binding.TraceID}, err)
	}
	p.abort(binding.TraceID)
	dir := filepath.Join(tracesRoot(p.stateDir), binding.TraceID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "raw.events" || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe trace cleanup entry")
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
