package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SnapshotSource interface {
	Snapshot(context.Context, core.Workspace) core.FastSnapshot
}

type CachedSnapshotSource interface {
	SnapshotCached(context.Context, core.Workspace) core.FastSnapshot
}

type FreshSnapshotSource interface {
	SnapshotFresh(context.Context, core.Workspace) core.FastSnapshot
}

type Observer struct {
	registry Registry
	source   SnapshotSource
	now      func() time.Time
}

func NewObserver(registry Registry, source SnapshotSource) *Observer {
	return &Observer{registry: registry, source: source, now: func() time.Time { return time.Now().UTC() }}
}

// Observe remains a compatibility alias until daemon callers migrate in Task 4.
func (o *Observer) Observe(ctx context.Context, cwd string) core.FastSnapshot {
	return o.ObserveFresh(ctx, cwd)
}

func (o *Observer) Bind(ctx context.Context, cwd string) core.Binding {
	workspace, ok, _ := o.resolve(ctx, cwd)
	if !ok {
		return core.Binding{}
	}
	return core.Binding{RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID}
}

func (o *Observer) ObserveCached(ctx context.Context, cwd string) core.FastSnapshot {
	now := o.now().UTC()
	workspace, ok, diagnostic := o.resolve(ctx, cwd)
	if !ok {
		return observerUnavailable(now, diagnostic)
	}
	source, ok := o.source.(CachedSnapshotSource)
	if !ok || source == nil {
		return boundObserverUnavailable(workspace, now, "workspace_cache_unavailable")
	}
	got := source.SnapshotCached(ctx, workspace)
	return bindSnapshot(got, workspace)
}

func (o *Observer) ObserveFresh(ctx context.Context, cwd string) core.FastSnapshot {
	now := o.now().UTC()
	workspace, ok, diagnostic := o.resolve(ctx, cwd)
	if !ok {
		return observerUnavailable(now, diagnostic)
	}
	if o.source == nil {
		return boundObserverUnavailable(workspace, now, "workspace_observer_unavailable")
	}
	var got core.FastSnapshot
	if source, ok := o.source.(FreshSnapshotSource); ok {
		got = source.SnapshotFresh(ctx, workspace)
	} else {
		got = o.source.Snapshot(ctx, workspace)
	}
	return bindSnapshot(got, workspace)
}

func (o *Observer) resolve(ctx context.Context, cwd string) (core.Workspace, bool, string) {
	if o.registry == nil {
		return core.Workspace{}, false, "workspace_observer_unavailable"
	}
	workspaces, err := o.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, false, "workspace_registry_unavailable"
	}
	workspace, ok := mostSpecificWorkspace(workspaces, cwd)
	if !ok {
		return core.Workspace{}, false, "workspace_unregistered"
	}
	return workspace, true, ""
}

func bindSnapshot(snapshot core.FastSnapshot, workspace core.Workspace) core.FastSnapshot {
	snapshot.RepositoryID = workspace.RepositoryID
	snapshot.WorkspaceID = workspace.ID
	return snapshot
}

func mostSpecificWorkspace(workspaces []core.Workspace, cwd string) (core.Workspace, bool) {
	cwd = filepath.Clean(cwd)
	var best core.Workspace
	bestLen := -1
	for _, candidate := range workspaces {
		root := filepath.Clean(candidate.Root)
		if !pathContains(root, cwd) || len(root) <= bestLen {
			continue
		}
		best = candidate
		bestLen = len(root)
	}
	return best, bestLen >= 0
}

func pathContains(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func boundObserverUnavailable(workspace core.Workspace, now time.Time, code string) core.FastSnapshot {
	got := observerUnavailable(now, code)
	got.RepositoryID = workspace.RepositoryID
	got.WorkspaceID = workspace.ID
	return got
}

func observerUnavailable(now time.Time, code string) core.FastSnapshot {
	return core.FastSnapshot{SchemaVersion: core.SnapshotSchemaVersion, Quality: core.QualityUnavailable, UpstreamQuality: core.QualityUnavailable, ObservedAt: now, DiagnosticCode: code}
}
