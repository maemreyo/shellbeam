package codeintel

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type InspectRequest struct {
	WorkspaceID string
	ActivityID  string
	Query       core.Query
}

type ServiceLimits struct {
	Delta                  workspacecore.DeltaLimits
	Result                 core.ResultLimits
	MaxSelectedSources     int
	MaxSelectedSourceBytes int64
	MaxDuration            time.Duration
}

type Service struct {
	workspaces WorkspaceLookup
	sampler    WorkspaceSampler
	activities ActivitySelector
	binder     SourceBinder
	providers  ProviderPool
	coherence  CoherenceSource
	limits     ServiceLimits
}

func NewService(workspaces WorkspaceLookup, sampler WorkspaceSampler, activities ActivitySelector, binder SourceBinder, providers ProviderPool, coherence CoherenceSource, limits ServiceLimits) (*Service, error) {
	limits.Delta = limits.Delta.Normalize()
	if workspaces == nil || binder == nil || providers == nil ||
		limits.Delta.Validate() != nil || limits.Result.Validate() != nil ||
		limits.MaxSelectedSources < 1 || limits.MaxSelectedSources > 4096 ||
		limits.MaxSelectedSourceBytes < 1 || limits.MaxSelectedSourceBytes > 64<<20 ||
		limits.MaxDuration <= 0 || limits.MaxDuration > 30*time.Second {
		return nil, fmt.Errorf("invalid code intelligence service config")
	}
	return &Service{workspaces: workspaces, sampler: sampler, activities: activities, binder: binder, providers: providers, coherence: coherence, limits: limits}, nil
}

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (core.Result, error) {
	if err := request.Validate(); err != nil {
		return core.Result{}, err
	}
	workspace, err := s.workspaces.Inspect(ctx, request.WorkspaceID)
	if err != nil {
		return core.Result{}, err
	}
	if err := workspace.Validate(); err != nil {
		return core.Result{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, s.limits.MaxDuration)
	defer cancel()

	sample := s.sample(queryCtx, workspace)
	selection, paths, selectionBounded, err := s.selectPaths(queryCtx, request, sample)
	if err != nil {
		return core.Result{}, err
	}
	if request.Query.Scope == core.ScopeChangedFiles && sample.Completeness == workspacecore.SelectionUnavailable {
		return core.Result{Status: core.StatusUnavailable, Query: request.Query, Selection: selection}, nil
	}
	selected, bindBounded, err := s.bindSelected(queryCtx, workspace, request.Query, paths)
	if err != nil {
		return core.Result{}, err
	}
	bounded := selectionBounded || bindBounded
	if bounded {
		degradeSelectionForBudget(&selection)
	}

	before := s.captureBarrier()
	response, err := s.providers.Query(queryCtx, ProviderRequest{Workspace: workspace, Sample: sample, SelectedSources: cloneBoundSources(selected), Query: request.Query})
	if err != nil {
		if queryCtx.Err() != nil {
			return core.Result{}, newError(CodeQueryBudgetExceeded, true, queryCtx.Err())
		}
		return core.Result{}, err
	}
	if queryCtx.Err() != nil {
		return core.Result{}, newError(CodeQueryBudgetExceeded, true, queryCtx.Err())
	}
	after := s.captureBarrier()
	selection.ManagedOverlap = selection.ManagedOverlap || before.ActiveManagedShellOperations > 0 || after.ActiveManagedShellOperations > 0

	changedRefs := s.recheckSelected(queryCtx, workspace, selected)
	if barriersDiffer(before, after) || len(changedRefs) != 0 {
		degradeSelectionForBarrier(&selection)
	}

	if err := response.Metadata.Validate(); err != nil {
		return core.Result{}, fmt.Errorf("invalid provider metadata: %w", err)
	}
	status := response.Status
	if status == "" {
		status = core.StatusReady
	}
	result := core.Result{Status: status, Query: request.Query, Selection: selection, Provider: response.Metadata}
	if bounded || selectionRequiresPartial(selection.Completeness) {
		result.Status = partialStatus(result.Status)
	}
	records, recordBounded, recordChanged, err := s.normalizeRecords(request.Query, response, selected, changedRefs)
	if err != nil {
		return core.Result{}, err
	}
	result.Records = records
	if recordBounded || recordChanged {
		result.Status = partialStatus(result.Status)
		if recordBounded {
			degradeSelectionForBudget(&result.Selection)
		}
	}
	return s.fitResult(result)
}

func (r InspectRequest) Validate() error {
	if _, err := workspacecore.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return err
	}
	if r.ActivityID != "" {
		if _, err := activitycore.ParseID(r.ActivityID); err != nil {
			return err
		}
	}
	return r.Query.Validate()
}

func (s *Service) sample(ctx context.Context, workspace workspacecore.Workspace) workspacecore.DeltaSample {
	if s.sampler != nil {
		return s.sampler.Sample(ctx, workspace.ID, s.limits.Delta)
	}
	return unavailableSample(workspace, "delta_sampler_unavailable")
}

func unavailableSample(workspace workspacecore.Workspace, diagnostic string) workspacecore.DeltaSample {
	return workspacecore.DeltaSample{
		SchemaVersion:  workspacecore.DeltaSampleSchemaVersion,
		RepositoryID:   workspace.RepositoryID,
		WorkspaceID:    workspace.ID,
		Freshness:      workspacecore.SampleFreshlySampled,
		Completeness:   workspacecore.SelectionUnavailable,
		ObservedAt:     time.Now().UTC(),
		DiagnosticCode: diagnostic,
		BarrierBefore:  workspacecore.CoherenceBarrier{DaemonIncarnation: "unmanaged"},
		BarrierAfter:   workspacecore.CoherenceBarrier{DaemonIncarnation: "unmanaged"},
	}
}

func (s *Service) selectPaths(ctx context.Context, request InspectRequest, sample workspacecore.DeltaSample) (core.SelectionMetadata, []string, bool, error) {
	if request.Query.Scope != core.ScopeChangedFiles {
		if queryNeedsBoundInput(request.Query) {
			return core.SelectionMetadata{}, []string{request.Query.Path}, false, nil
		}
		return core.SelectionMetadata{}, nil, false, nil
	}
	selection := core.SelectionMetadata{
		Basis:          workspacecore.SelectionWorkspaceDirty,
		Freshness:      sample.Freshness,
		Completeness:   sample.Completeness,
		ManagedOverlap: sample.BarrierBefore.ActiveManagedShellOperations > 0 || sample.BarrierAfter.ActiveManagedShellOperations > 0,
	}
	paths := workspaceDirtyPaths(sample)
	if request.ActivityID != "" {
		selection.Basis = workspacecore.SelectionActivityDelta
		if s.activities == nil {
			return core.SelectionMetadata{}, nil, false, fmt.Errorf("activity selection unavailable")
		}
		comparison, err := s.activities.CompareWorkspace(ctx, request.ActivityID, sample)
		if err != nil {
			return core.SelectionMetadata{}, nil, false, err
		}
		if comparison.BaselineDiverged {
			selection.Completeness = workspacecore.SelectionDiverged
			selection.Fallback = string(workspacecore.SelectionWorkspaceDirty)
		} else {
			paths = activityObservedPaths(comparison.ObservedSinceBaseline)
		}
	}
	bounded := len(paths) > s.limits.MaxSelectedSources
	if bounded {
		paths = append([]string(nil), paths[:s.limits.MaxSelectedSources]...)
	}
	return selection, paths, bounded, nil
}

func queryNeedsBoundInput(query core.Query) bool {
	if query.Path == "" {
		return false
	}
	if query.Scope == core.ScopeFile {
		return true
	}
	switch query.Kind {
	case core.QueryDefinition, core.QueryReferences, core.QueryTypeDefinition, core.QueryTypeSummary, core.QueryCallers, core.QueryCallees:
		return true
	default:
		return false
	}
}

func (s *Service) bindSelected(ctx context.Context, workspace workspacecore.Workspace, query core.Query, paths []string) ([]BoundSource, bool, error) {
	selected := make([]BoundSource, 0, len(paths))
	var bytesUsed int64
	bounded := false
	for _, path := range paths {
		bound, err := s.binder.Bind(ctx, workspace, path)
		if err != nil {
			if query.Scope == core.ScopeFile || queryNeedsBoundInput(query) {
				return nil, false, err
			}
			bounded = true
			continue
		}
		if bytesUsed+int64(len(bound.Bytes)) > s.limits.MaxSelectedSourceBytes {
			bounded = true
			break
		}
		bytesUsed += int64(len(bound.Bytes))
		selected = append(selected, cloneBoundSource(bound))
	}
	return selected, bounded, nil
}

func (s *Service) recheckSelected(ctx context.Context, workspace workspacecore.Workspace, selected []BoundSource) map[core.SourceRefID]bool {
	changed := make(map[core.SourceRefID]bool)
	for _, original := range selected {
		current, err := s.binder.Bind(ctx, workspace, original.Ref.LogicalPath)
		if err != nil || !bytes.Equal(current.Bytes, original.Bytes) {
			changed[original.Ref.ID] = true
		}
	}
	return changed
}

func workspaceDirtyPaths(sample workspacecore.DeltaSample) []string {
	set := make(map[string]struct{}, len(sample.Changes))
	for _, change := range sample.Changes {
		if change.PathTransition == workspacecore.PathDeleted || change.NewPath == "" {
			continue
		}
		set[change.NewPath] = struct{}{}
	}
	return sortedKeys(set)
}

func activityObservedPaths(facts []activitycore.PathFact) []string {
	set := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		if fact.State == activitycore.PathDeleted || fact.Path == "" {
			continue
		}
		set[fact.Path] = struct{}{}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneBoundSources(sources []BoundSource) []BoundSource {
	out := make([]BoundSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, cloneBoundSource(source))
	}
	return out
}

func (s *Service) captureBarrier() workspacecore.CoherenceBarrier {
	if s.coherence == nil {
		return workspacecore.CoherenceBarrier{DaemonIncarnation: "unmanaged"}
	}
	return s.coherence.CaptureBarrier()
}

func barriersDiffer(a, b workspacecore.CoherenceBarrier) bool {
	return a.DaemonIncarnation != b.DaemonIncarnation || a.Epoch != b.Epoch || a.ActiveManagedShellOperations != b.ActiveManagedShellOperations
}

func degradeSelectionForBudget(selection *core.SelectionMetadata) {
	if selection == nil || selection.Completeness == "" || selection.Completeness == workspacecore.SelectionDiverged || selection.Completeness == workspacecore.SelectionUnavailable {
		return
	}
	selection.Completeness = workspacecore.SelectionPartial
}

func degradeSelectionForBarrier(selection *core.SelectionMetadata) {
	if selection == nil || selection.Completeness == "" || selection.Completeness == workspacecore.SelectionDiverged || selection.Completeness == workspacecore.SelectionUnavailable {
		return
	}
	selection.Completeness = workspacecore.SelectionPotentiallyStale
}

func selectionRequiresPartial(completeness workspacecore.SelectionCompleteness) bool {
	switch completeness {
	case workspacecore.SelectionPartial, workspacecore.SelectionDiverged, workspacecore.SelectionPotentiallyStale:
		return true
	default:
		return false
	}
}

func partialStatus(status core.ResultStatus) core.ResultStatus {
	switch status {
	case core.StatusReady, core.StatusPartial:
		return core.StatusPartial
	default:
		return status
	}
}
