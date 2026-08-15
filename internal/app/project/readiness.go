package project

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	DefaultReadinessTTL        = 30 * time.Second
	DefaultReadinessMaxEntries = 256
)

type ReadinessObservers struct {
	Executable  ExecutableObserver
	Environment EnvironmentObserver
	Toolchain   ToolchainObserver
}

type ReadinessOptions struct {
	TTL        time.Duration
	MaxEntries int
	Now        func() time.Time
}

type readinessCacheEntry struct {
	value core.Readiness
}

type readinessRuntime struct {
	observers  ReadinessObservers
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	mu         sync.Mutex
	cache      map[string]readinessCacheEntry
}

func NewWithReadiness(workspaces WorkspaceLookup, loader Loader, reviews ReviewStore, observers ReadinessObservers, options ReadinessOptions) *Service {
	svc := New(workspaces, loader, reviews)
	ttl := options.TTL
	if ttl <= 0 {
		ttl = DefaultReadinessTTL
	}
	maxEntries := options.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultReadinessMaxEntries
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	svc.readiness = &readinessRuntime{
		observers: observers, ttl: ttl, maxEntries: maxEntries, now: now,
		cache: make(map[string]readinessCacheEntry, maxEntries),
	}
	return svc
}

func (s *Service) Readiness(ctx context.Context, workspaceID string) (core.Readiness, error) {
	if s == nil || s.readiness == nil {
		return core.Readiness{}, failure.New(failure.ProjectReadinessUnavailable, map[string]string{"workspace_id": workspaceID, "reason": "observer_not_configured"}, nil)
	}
	if err := ctx.Err(); err != nil {
		return core.Readiness{}, err
	}
	record, err := s.workspace(ctx, workspaceID)
	if err != nil {
		return core.Readiness{}, err
	}
	load := s.loader.Load(ctx, record.Root)
	if load.State != core.LoadValid || load.Parsed == nil || load.ManifestDigest == "" {
		return core.Readiness{}, failure.New(failure.ProjectReadinessUnavailable, map[string]string{"workspace_id": workspaceID, "reason": "manifest_unavailable"}, nil)
	}
	now := s.readiness.now().UTC()
	key := readinessCacheKey(record, load)
	if cached, ok := s.readiness.cached(key, now); ok {
		return cached, nil
	}
	value, err := s.evaluateReadiness(ctx, record, load, now)
	if err != nil {
		return core.Readiness{}, err
	}
	if err := value.Validate(); err != nil {
		return core.Readiness{}, failure.New(failure.ProjectReadinessUnavailable, map[string]string{"workspace_id": workspaceID, "reason": "invalid_observation"}, err)
	}
	s.readiness.store(key, value)
	return cloneReadiness(value), nil
}

func (s *Service) evaluateReadiness(ctx context.Context, record workspace.Workspace, load core.LoadResult, now time.Time) (core.Readiness, error) {
	manifest := load.Parsed.Manifest
	value := core.Readiness{
		SchemaVersion: core.ReadinessSchemaVersion, RepositoryID: string(record.RepositoryID), WorkspaceID: string(record.ID),
		ManifestDigest: load.ManifestDigest, ManifestSchemaVersion: manifest.SchemaVersion,
		CapturedAt: now, CacheQuality: core.CacheFresh,
	}
	if manifest.SchemaVersion < core.ManifestSchemaV2 || !hasReadinessRequirements(manifest.Requirements) {
		value.State = core.ReadinessUnavailable
		return value, nil
	}
	checks, err := s.observeRequirements(ctx, record.Root, manifest)
	if err != nil {
		return core.Readiness{}, err
	}
	value.Checks = checks
	value.State = core.FoldReadiness(checks)
	value.EnvironmentFingerprint, err = readinessKindFingerprint(checks, core.RequirementEnvironmentPresence)
	if err != nil {
		return core.Readiness{}, err
	}
	value.ToolchainFingerprint, err = readinessKindFingerprint(checks, core.RequirementToolchain)
	if err != nil {
		return core.Readiness{}, err
	}
	return value, nil
}

func (s *Service) observeRequirements(ctx context.Context, root string, manifest core.Manifest) ([]core.ReadinessCheck, error) {
	checks := make([]core.ReadinessCheck, 0,
		len(manifest.Requirements.Toolchains)+len(manifest.Requirements.Executables)+
			len(manifest.Requirements.Environment.RequiredPresence)+len(manifest.Requirements.Environment.OptionalPresence))
	for _, id := range sortedToolchainRequirementIDs(manifest.Requirements.Toolchains) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requirement := manifest.Requirements.Toolchains[id]
		observed := core.ReadinessCheck{Status: core.CheckUnavailable}
		if s.readiness.observers.Toolchain != nil {
			observed = s.readiness.observers.Toolchain.ObserveToolchain(ctx, root, id, manifest.Toolchains[id])
		}
		checks = append(checks, normalizeObservedCheck(id, core.RequirementToolchain, requirement.Required, observed))
	}
	for _, id := range sortedExecutableRequirementIDs(manifest.Requirements.Executables) {
		requirement := manifest.Requirements.Executables[id]
		observed := core.ReadinessCheck{Status: core.CheckUnavailable}
		if s.readiness.observers.Executable != nil {
			observed = s.readiness.observers.Executable.ObserveExecutable(ctx, id)
		}
		checks = append(checks, normalizeObservedCheck(id, core.RequirementExecutable, requirement.Required, observed))
	}
	checks = appendEnvironmentChecks(ctx, checks, s.readiness.observers.Environment, manifest.Requirements.Environment.RequiredPresence, true)
	checks = appendEnvironmentChecks(ctx, checks, s.readiness.observers.Environment, manifest.Requirements.Environment.OptionalPresence, false)
	return checks, ctx.Err()
}

func appendEnvironmentChecks(ctx context.Context, checks []core.ReadinessCheck, observer EnvironmentObserver, names []string, required bool) []core.ReadinessCheck {
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	for _, id := range ordered {
		if ctx.Err() != nil {
			return checks
		}
		observed := core.ReadinessCheck{Status: core.CheckUnavailable}
		if observer != nil {
			observed = observer.ObserveEnvironmentPresence(ctx, id, required)
		}
		checks = append(checks, normalizeObservedCheck(id, core.RequirementEnvironmentPresence, required, observed))
	}
	return checks
}

func normalizeObservedCheck(id string, kind core.RequirementKind, required bool, observed core.ReadinessCheck) core.ReadinessCheck {
	check := core.ReadinessCheck{
		ID: id, Kind: kind, Required: required, Status: observed.Status,
		ProviderID: observed.ProviderID, ProviderVersion: observed.ProviderVersion,
	}
	if err := check.Validate(); err != nil {
		check.Status, check.ProviderID, check.ProviderVersion = core.CheckUnavailable, "", 0
	}
	check.Code = readinessCheckCode(kind, check.Status)
	return check
}

func readinessCheckCode(kind core.RequirementKind, status core.CheckStatus) string {
	if kind == core.RequirementToolchain {
		switch status {
		case core.CheckMissing:
			return string(failure.ToolchainMissing)
		case core.CheckUnknown:
			return string(failure.ToolchainVersionUnknown)
		case core.CheckIncompatible:
			return string(failure.ToolchainIncompatible)
		}
	}
	if status == core.CheckUnavailable {
		return string(failure.ProjectReadinessUnavailable)
	}
	return ""
}

func readinessKindFingerprint(checks []core.ReadinessCheck, kind core.RequirementKind) (string, error) {
	filtered := make([]core.ReadinessCheck, 0, len(checks))
	for _, check := range checks {
		if check.Kind == kind {
			filtered = append(filtered, check)
		}
	}
	if len(filtered) == 0 {
		return "", nil
	}
	return core.ReadinessChecksFingerprint(filtered)
}

func hasReadinessRequirements(requirements core.Requirements) bool {
	return len(requirements.Toolchains) > 0 || len(requirements.Executables) > 0 ||
		len(requirements.Environment.RequiredPresence) > 0 || len(requirements.Environment.OptionalPresence) > 0
}

func sortedToolchainRequirementIDs(values map[string]core.ToolchainRequirement) []string {
	out := make([]string, 0, len(values))
	for id := range values {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortedExecutableRequirementIDs(values map[string]core.ExecutableRequirement) []string {
	out := make([]string, 0, len(values))
	for id := range values {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func readinessCacheKey(record workspace.Workspace, load core.LoadResult) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", record.RepositoryID, record.ID, load.ManifestDigest, load.Parsed.Manifest.SchemaVersion)
}

func (r *readinessRuntime) cached(key string, now time.Time) (core.Readiness, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return core.Readiness{}, false
	}
	age := now.Sub(entry.value.CapturedAt)
	if age < 0 || age > r.ttl {
		delete(r.cache, key)
		return core.Readiness{}, false
	}
	value := cloneReadiness(entry.value)
	value.CacheQuality = core.CacheCached
	value.CacheAgeMS = age.Milliseconds()
	return value, true
}

func (r *readinessRuntime) store(key string, value core.Readiness) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cache[key]; !exists && len(r.cache) >= r.maxEntries {
		r.evictOldestLocked()
	}
	r.cache[key] = readinessCacheEntry{value: cloneReadiness(value)}
}

func (r *readinessRuntime) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range r.cache {
		captured := entry.value.CapturedAt
		if oldestKey == "" || captured.Before(oldest) || captured.Equal(oldest) && key < oldestKey {
			oldestKey, oldest = key, captured
		}
	}
	if oldestKey != "" {
		delete(r.cache, oldestKey)
	}
}

func cloneReadiness(value core.Readiness) core.Readiness {
	value.Checks = append([]core.ReadinessCheck(nil), value.Checks...)
	return value
}
