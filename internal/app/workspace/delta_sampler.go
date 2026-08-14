package workspace

import (
	"context"
	"sort"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const maxDeltaSampleCacheWorkspaces = 64

type DeltaSource interface {
	SampleDelta(context.Context, core.Workspace, core.DeltaLimits) core.DeltaSample
}

type DeltaCoherence interface {
	CaptureBarrier() core.CoherenceBarrier
}

type DeltaSampler struct {
	registry  Registry
	source    DeltaSource
	coherence DeltaCoherence

	mu    sync.Mutex
	last  map[core.WorkspaceID]core.DeltaSample
	order []core.WorkspaceID
}

func NewDeltaSampler(registry Registry, source DeltaSource, coherence DeltaCoherence) *DeltaSampler {
	return &DeltaSampler{registry: registry, source: source, coherence: coherence, last: make(map[core.WorkspaceID]core.DeltaSample)}
}

func (s *DeltaSampler) Sample(ctx context.Context, workspaceID core.WorkspaceID, limits core.DeltaLimits) core.DeltaSample {
	limits = limits.Normalize()
	if limits.Validate() != nil {
		return s.unavailable(workspaceID, core.RepositoryID(""), "delta_limits_invalid")
	}
	workspace, ok := s.lookupWorkspace(ctx, workspaceID)
	if !ok {
		return s.unavailable(workspaceID, core.RepositoryID(""), "workspace_unavailable")
	}
	budgetCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutMS)*time.Millisecond)
	defer cancel()

	sample := s.sampleOnce(budgetCtx, workspace, limits)
	if sample.Completeness == core.SelectionPotentiallyStale && limits.RequireComplete && budgetCtx.Err() == nil {
		sample = s.sampleOnce(budgetCtx, workspace, limits)
	}
	s.deriveTransitions(&sample)
	s.remember(sample)
	return sample
}

func (s *DeltaSampler) sampleOnce(ctx context.Context, workspace core.Workspace, limits core.DeltaLimits) core.DeltaSample {
	before := s.captureBarrier()
	if s.source == nil {
		got := s.unavailable(workspace.ID, workspace.RepositoryID, "delta_source_unavailable")
		got.BarrierBefore, got.BarrierAfter = before, s.captureBarrier()
		return got
	}
	got := s.source.SampleDelta(ctx, workspace, limits)
	after := s.captureBarrier()
	got.BarrierBefore, got.BarrierAfter = before, after
	stable := before.DaemonIncarnation == after.DaemonIncarnation && before.Epoch == after.Epoch
	overlap := before.ActiveManagedShellOperations != 0 || after.ActiveManagedShellOperations != 0
	if got.Completeness == core.SelectionComplete && got.SelectionModes.PotentiallyIncomplete() {
		got.Completeness = core.SelectionPartial
		if got.DiagnosticCode == "" {
			got.DiagnosticCode = "repository_mode_potentially_incomplete"
		}
	}
	if got.Completeness == core.SelectionComplete && !stable {
		got.Completeness = core.SelectionPotentiallyStale
		if got.DiagnosticCode == "" {
			got.DiagnosticCode = "managed_shell_epoch_changed"
		}
	}
	got.CacheEligible = got.Completeness == core.SelectionComplete && stable && !overlap
	return got
}

func (s *DeltaSampler) deriveTransitions(current *core.DeltaSample) {
	if current == nil || current.Completeness == core.SelectionUnavailable {
		return
	}
	s.mu.Lock()
	previous, ok := s.last[current.WorkspaceID]
	s.mu.Unlock()
	if !ok {
		return
	}
	if previous.Head != current.Head || previous.Ref != current.Ref || previous.Detached != current.Detached || previous.Unborn != current.Unborn {
		current.SourceViewMayHaveChanged = true
		return
	}
	previousDirty := deltaChangesByPath(previous.Changes)
	currentDirty := deltaChangesByPath(current.Changes)
	for path := range previousDirty {
		if _, exists := currentDirty[path]; !exists {
			current.ResolvedPaths = append(current.ResolvedPaths, path)
		}
	}
	sort.Strings(current.ResolvedPaths)
	if len(current.ResolvedPaths) != 0 || !sameDeltaChanges(previousDirty, currentDirty) {
		current.SourceViewMayHaveChanged = true
	}
}

func (s *DeltaSampler) remember(sample core.DeltaSample) {
	if !sample.CacheEligible {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.last[sample.WorkspaceID]; !exists {
		if len(s.order) >= maxDeltaSampleCacheWorkspaces {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.last, oldest)
		}
		s.order = append(s.order, sample.WorkspaceID)
	}
	s.last[sample.WorkspaceID] = boundedDeltaSummary(sample)
}

func (s *DeltaSampler) lookupWorkspace(ctx context.Context, id core.WorkspaceID) (core.Workspace, bool) {
	if s.registry == nil {
		return core.Workspace{}, false
	}
	workspaces, err := s.registry.ListWorkspaces(ctx)
	if err != nil {
		return core.Workspace{}, false
	}
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return core.Workspace{}, false
}

func (s *DeltaSampler) captureBarrier() core.CoherenceBarrier {
	if s.coherence == nil {
		return core.CoherenceBarrier{DaemonIncarnation: "unmanaged"}
	}
	return s.coherence.CaptureBarrier()
}

func (s *DeltaSampler) unavailable(workspaceID core.WorkspaceID, repositoryID core.RepositoryID, diagnostic string) core.DeltaSample {
	return core.DeltaSample{SchemaVersion: core.DeltaSampleSchemaVersion, RepositoryID: repositoryID, WorkspaceID: workspaceID, Freshness: core.SampleFreshlySampled, Completeness: core.SelectionUnavailable, ObservedAt: time.Now().UTC(), DiagnosticCode: diagnostic}
}

func deltaChangesByPath(changes []core.ChangeRecord) map[string]core.ChangeRecord {
	out := make(map[string]core.ChangeRecord, len(changes))
	for _, change := range changes {
		path := change.NewPath
		if path == "" {
			path = change.OldPath
		}
		if path != "" {
			out[path] = change
		}
	}
	return out
}

func sameDeltaChanges(a, b map[string]core.ChangeRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for path, left := range a {
		if right, ok := b[path]; !ok || left != right {
			return false
		}
	}
	return true
}

func boundedDeltaSummary(sample core.DeltaSample) core.DeltaSample {
	sample.Changes = append([]core.ChangeRecord(nil), sample.Changes...)
	sample.ResolvedPaths = append([]string(nil), sample.ResolvedPaths...)
	return sample
}
