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

type Observer struct {
	registry Registry
	source   SnapshotSource
	now      func() time.Time
}

func NewObserver(registry Registry, source SnapshotSource) *Observer {
	return &Observer{registry: registry, source: source, now: func() time.Time { return time.Now().UTC() }}
}

func (o *Observer) Observe(ctx context.Context, cwd string) core.FastSnapshot {
	now := o.now().UTC()
	if o.registry == nil || o.source == nil {
		return observerUnavailable(now, "workspace_observer_unavailable")
	}
	workspaces, err := o.registry.ListWorkspaces(ctx)
	if err != nil {
		return observerUnavailable(now, "workspace_registry_unavailable")
	}
	workspace, ok := mostSpecificWorkspace(workspaces, cwd)
	if !ok {
		return observerUnavailable(now, "workspace_unregistered")
	}
	got := o.source.Snapshot(ctx, workspace)
	got.RepositoryID = workspace.RepositoryID
	got.WorkspaceID = workspace.ID
	return got
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

func observerUnavailable(now time.Time, code string) core.FastSnapshot {
	return core.FastSnapshot{SchemaVersion: core.SnapshotSchemaVersion, Quality: core.QualityUnavailable, UpstreamQuality: core.QualityUnavailable, ObservedAt: now, DiagnosticCode: code}
}
