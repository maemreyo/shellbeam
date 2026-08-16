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

// Discoverer finds the repository a path belongs to.
//
// A caller that has not registered anything still knows where it is working,
// and until now that was not enough: an unregistered directory produced no
// provenance at all, so an agent had to learn ShellBeam's workspace ids before
// it could run its first command and get a usable answer. Discovery closes that
// gap without changing what registration means -- what is discovered is
// reported, not recorded.
type Discoverer interface {
	Inspect(context.Context, string) (GitObservation, error)
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
	// Registry only. A binding is what a session records, and discovery has no
	// identity to contribute to it.
	workspace, ok, _ := o.resolve(ctx, cwd, withoutDiscovery)
	if !ok {
		return core.Binding{}
	}
	return core.Binding{RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID}
}

func (o *Observer) ObserveCached(ctx context.Context, cwd string) core.FastSnapshot {
	now := o.now().UTC()
	// Registry only: this runs on the ordinary execution path, and asking git
	// about every directory a command happens to use would be work the caller
	// never asked for and cannot see.
	workspace, ok, diagnostic := o.resolve(ctx, cwd, withoutDiscovery)
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
	// A fresh observation is something a caller asked for explicitly, so this is
	// where looking at an unregistered directory is warranted.
	workspace, ok, diagnostic := o.resolve(ctx, cwd, withDiscovery)
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

// Discovery is available only where a caller asked for a fresh observation.
// The ordinary execution path resolves from the registry alone, so running a
// command never shells out to git behind the caller's back.
const (
	withDiscovery    = true
	withoutDiscovery = false
)

func (o *Observer) resolve(ctx context.Context, cwd string, allowDiscovery bool) (core.Workspace, bool, string) {
	if o.registry == nil {
		return core.Workspace{}, false, "workspace_observer_unavailable"
	}
	workspaces, err := o.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, false, "workspace_registry_unavailable"
	}
	if workspace, ok := mostSpecificWorkspace(workspaces, cwd); ok {
		return workspace, true, ""
	}
	// Nothing registered covers this directory. Ask git what is actually there
	// rather than refusing: the repository, worktree and branch are observable
	// from the path itself, and reporting them costs the caller nothing it has
	// to undo.
	if allowDiscovery {
		if discovered, ok := o.discover(ctx, cwd); ok {
			return discovered, true, ""
		}
	}
	return core.Workspace{}, false, "workspace_unregistered"
}

// discover builds an unregistered workspace from what git can see at cwd.
//
// It has no identifiers, and that absence is the point: identifiers are what
// registration confers, and inventing one here would make a directory an agent
// happened to name indistinguishable from one an operator chose to track. The
// provenance is real; the registration is not claimed.
func (o *Observer) discover(ctx context.Context, cwd string) (core.Workspace, bool) {
	discoverer, ok := o.source.(Discoverer)
	if !ok || discoverer == nil || cwd == "" {
		return core.Workspace{}, false
	}
	observation, err := discoverer.Inspect(ctx, cwd)
	if err != nil || observation.Root == "" || observation.GitDir == "" {
		return core.Workspace{}, false
	}
	return core.Workspace{
		SchemaVersion: core.SchemaVersion,
		Root:          observation.Root,
		GitDir:        observation.GitDir,
		Branch:        observation.Branch,
	}, true
}

// registered reports whether a resolved workspace came from the registry.
func registered(workspace core.Workspace) bool { return workspace.ID != "" }

func bindSnapshot(snapshot core.FastSnapshot, workspace core.Workspace) core.FastSnapshot {
	snapshot.RepositoryID = workspace.RepositoryID
	snapshot.WorkspaceID = workspace.ID
	if !registered(workspace) && snapshot.DiagnosticCode == "" {
		// The observation is real but nothing here is registered. Saying so
		// alongside the provenance is different from saying it instead of the
		// provenance, which is what a caller used to get.
		snapshot.DiagnosticCode = "workspace_unregistered"
	}
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
