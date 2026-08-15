package environment

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

const DefaultMaxCacheEntries = 128

var builtInPresenceNames = []string{"CI", "SHELL", "TERM"}

type HostObserver interface {
	Observe(context.Context, core.ExecutionContext, []string) (core.FingerprintInput, error)
}

type ManifestProvider interface {
	Manifest(context.Context, string) (ManifestView, error)
}

type ToolchainProber interface {
	Probe(context.Context, string, string, project.Toolchain) core.ToolchainObservation
}

type ManifestView struct {
	WorkspaceID    string
	ManifestDigest string
	Manifest       project.Manifest
}

type ToolchainRequest struct {
	Kind              string
	RequestedIdentity string
	Declaration       project.Toolchain
}

type InspectRequest struct {
	WorkspaceID string
	Freshness   core.Freshness
	Execution   *core.ExecutionContext
}

type BindingRequest struct {
	WorkspaceID    string
	ManifestDigest string
	Execution      core.ExecutionContext
}

type Options struct {
	MaxEntries       int
	Now              func() time.Time
	DefaultExecution core.ExecutionContext
}

type Service struct {
	host             HostObserver
	manifests        ManifestProvider
	prober           ToolchainProber
	now              func() time.Time
	defaultExecution core.ExecutionContext
	cache            *snapshotCache
}

func NewService(host HostObserver, manifests ManifestProvider, prober ToolchainProber, options Options) *Service {
	maxEntries := options.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxCacheEntries
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		host: host, manifests: manifests, prober: prober, now: now,
		defaultExecution: options.DefaultExecution,
		cache:            newSnapshotCache(maxEntries),
	}
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (core.Snapshot, error) {
	if s == nil || s.host == nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "observer_not_configured"}, nil)
	}
	if err := ctx.Err(); err != nil {
		return core.Snapshot{}, err
	}
	freshness := request.Freshness
	if freshness == "" {
		freshness = core.FreshnessCached
	}
	if freshness != core.FreshnessCached && freshness != core.FreshnessRefresh {
		return core.Snapshot{}, failure.New(failure.InvalidInput, map[string]string{"field": "freshness"}, nil)
	}
	execution := s.defaultExecution
	if request.Execution != nil {
		execution = *request.Execution
	}
	if err := validateExecution(execution); err != nil {
		return core.Snapshot{}, failure.New(failure.InvalidInput, map[string]string{"field": "execution"}, err)
	}
	view, err := s.manifestView(ctx, request.WorkspaceID)
	if err != nil {
		return core.Snapshot{}, err
	}
	names, err := selectedPresenceNames(view.Manifest.RelevantEnvironment)
	if err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "selection_invalid"}, err)
	}
	toolchainRequests, err := selectedToolchains(view.Manifest.Toolchains)
	if err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "selection_invalid"}, err)
	}
	key, err := snapshotCacheKey(request.WorkspaceID, view.ManifestDigest, execution, names, toolchainRequests)
	if err != nil {
		return core.Snapshot{}, err
	}
	if freshness == core.FreshnessCached {
		if cached, ok := s.cache.get(key); ok {
			return cached, nil
		}
	}
	return s.capture(ctx, request.WorkspaceID, view.ManifestDigest, view.Manifest.Toolchains, execution, names, toolchainRequests, key)

}

func (s *Service) capture(
	ctx context.Context,
	workspaceID, manifestDigest string,
	declared map[string]project.Toolchain,
	execution core.ExecutionContext,
	names []string,
	requests []ToolchainRequest,
	cacheKey string,
) (core.Snapshot, error) {
	input, err := s.host.Observe(ctx, execution, names)
	if err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "host_observation_failed"}, err)
	}
	input.ToolchainManager = declaredManager(declared)
	environmentFingerprint, err := core.EnvironmentFingerprint(input)
	if err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "invalid_observation"}, err)
	}
	toolchains := s.observeToolchains(ctx, requests)
	toolchainFingerprint, toolchainVersion, quality, err := fingerprintToolchains(toolchains)
	if err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "invalid_toolchain_observation"}, err)
	}
	capturedAt := s.now().UTC()
	snapshot := core.Snapshot{
		SchemaVersion: core.SnapshotSchemaVersion, CapturedAt: capturedAt, Quality: quality,
		EnvironmentFingerprint: environmentFingerprint, FingerprintVersion: core.FingerprintVersion,
		ToolchainFingerprint: toolchainFingerprint, ToolchainFingerprintVersion: toolchainVersion,
		Platform: input.Platform, Execution: input.Execution, Path: input.Path,
		VariablePresence: append([]core.VariablePresence(nil), input.VariablePresence...),
		ToolchainManager: cloneManager(input.ToolchainManager), Toolchains: append([]core.ToolchainObservation(nil), toolchains...),
	}
	snapshot.SnapshotID = core.SnapshotID(capturedAt, environmentFingerprint, toolchainFingerprint)
	if err := snapshot.Validate(); err != nil {
		return core.Snapshot{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "invalid_snapshot"}, err)
	}
	bindingKey, err := cachedBindingKey(BindingRequest{WorkspaceID: workspaceID, ManifestDigest: manifestDigest, Execution: execution})
	if err != nil {
		return core.Snapshot{}, err
	}
	s.cache.put(cacheKey, bindingKey, snapshot)
	return cloneSnapshot(snapshot), nil
}

func fingerprintToolchains(toolchains []core.ToolchainObservation) (string, int, core.Quality, error) {
	if len(toolchains) == 0 {
		return "", 0, core.QualityComplete, nil
	}
	fingerprint, err := core.ToolchainFingerprint(toolchains)
	if err != nil {
		return "", 0, "", err
	}
	quality := core.QualityComplete
	for _, observed := range toolchains {
		if observed.Quality != core.ProbeComplete {
			quality = core.QualityPartial
			break
		}
	}
	return fingerprint, core.ToolchainFingerprintVersion, quality, nil
}

func (s *Service) CachedBinding(request BindingRequest) (core.Binding, bool) {
	if s == nil || s.cache == nil {
		return core.Binding{}, false
	}
	key, err := cachedBindingKey(request)
	if err != nil {
		return core.Binding{}, false
	}
	snapshot, ok := s.cache.getByBinding(key)
	if !ok {
		return core.Binding{}, false
	}
	binding := snapshot.Binding()
	if err := binding.Validate(); err != nil {
		return core.Binding{}, false
	}
	return binding, true
}

func (s *Service) CacheSize() int {
	if s == nil || s.cache == nil {
		return 0
	}
	return s.cache.size()
}

func (s *Service) manifestView(ctx context.Context, workspaceID string) (ManifestView, error) {
	if workspaceID == "" {
		return ManifestView{}, nil
	}
	if s.manifests == nil {
		return ManifestView{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "manifest_provider_unavailable"}, nil)
	}
	view, err := s.manifests.Manifest(ctx, workspaceID)
	if err != nil {
		return ManifestView{}, err
	}
	if view.ManifestDigest == "" {
		return ManifestView{}, failure.New(failure.EnvironmentObservationUnavailable, map[string]string{"reason": "manifest_unavailable"}, nil)
	}
	return view, nil
}

func (s *Service) observeToolchains(ctx context.Context, requests []ToolchainRequest) []core.ToolchainObservation {
	if s.prober == nil {
		return nil
	}
	out := make([]core.ToolchainObservation, 0, len(requests))
	for _, request := range requests {
		if ctx.Err() != nil {
			break
		}
		observed := s.prober.Probe(ctx, request.Kind, request.RequestedIdentity, request.Declaration)
		observed.Kind = request.Kind
		observed.RequestedIdentity = request.RequestedIdentity
		out = append(out, observed)
	}
	return out
}
