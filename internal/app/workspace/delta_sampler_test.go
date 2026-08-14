package workspace

import (
	"context"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDeltaSamplerDirtyToCleanReportsResolvedPath(t *testing.T) {
	workspace := deltaSamplerWorkspace()
	source := &sequenceDeltaSource{samples: []core.DeltaSample{
		deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main", modifiedDelta("foo.go")),
		deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main"),
	}}
	coherence := &sequenceDeltaCoherence{barriers: repeatedBarrier(4, 1, 0)}
	sampler := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, coherence)
	limits := core.DeltaLimits{}
	first := sampler.Sample(context.Background(), workspace.ID, limits)
	second := sampler.Sample(context.Background(), workspace.ID, limits)
	if !first.CacheEligible || !second.CacheEligible {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if len(second.Changes) != 0 || len(second.ResolvedPaths) != 1 || second.ResolvedPaths[0] != "foo.go" || !second.SourceViewMayHaveChanged {
		t.Fatalf("second=%#v", second)
	}
}

func TestDeltaSamplerCleanBranchAndHeadMovementDoesNotInventSourceChange(t *testing.T) {
	workspace := deltaSamplerWorkspace()
	tests := []struct {
		name       string
		secondHead string
		secondRef  string
	}{
		{"branch_switch", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "refs/heads/topic"},
		{"head_only", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "refs/heads/main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := &sequenceDeltaSource{samples: []core.DeltaSample{
				deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main"),
				deltaSourceSample(workspace, tc.secondHead, tc.secondRef),
			}}
			sampler := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, &sequenceDeltaCoherence{barriers: repeatedBarrier(4, 1, 0)})
			_ = sampler.Sample(context.Background(), workspace.ID, core.DeltaLimits{})
			second := sampler.Sample(context.Background(), workspace.ID, core.DeltaLimits{})
			if !second.SourceViewMayHaveChanged || len(second.Changes) != 0 || len(second.ResolvedPaths) != 0 {
				t.Fatalf("second=%#v", second)
			}
		})
	}
}

func TestDeltaSamplerManagedOverlapDisablesCacheWithoutDiscardingSample(t *testing.T) {
	workspace := deltaSamplerWorkspace()
	source := &sequenceDeltaSource{samples: []core.DeltaSample{deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main", modifiedDelta("foo.go"))}}
	barrier := core.CoherenceBarrier{DaemonIncarnation: "d", Epoch: 7, ActiveManagedShellOperations: 1}
	sampler := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, &sequenceDeltaCoherence{barriers: []core.CoherenceBarrier{barrier, barrier}})
	got := sampler.Sample(context.Background(), workspace.ID, core.DeltaLimits{})
	if got.Completeness != core.SelectionComplete || got.CacheEligible || len(got.Changes) != 1 {
		t.Fatalf("sample=%#v", got)
	}
}

func TestDeltaSamplerEpochChangeIsPotentiallyStaleOrRetriedForComplete(t *testing.T) {
	workspace := deltaSamplerWorkspace()
	t.Run("best_effort", func(t *testing.T) {
		source := &sequenceDeltaSource{samples: []core.DeltaSample{deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main")}}
		barriers := []core.CoherenceBarrier{{DaemonIncarnation: "d", Epoch: 1}, {DaemonIncarnation: "d", Epoch: 2}}
		got := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, &sequenceDeltaCoherence{barriers: barriers}).Sample(context.Background(), workspace.ID, core.DeltaLimits{})
		if got.Completeness != core.SelectionPotentiallyStale || got.CacheEligible || source.calls != 1 {
			t.Fatalf("sample=%#v calls=%d", got, source.calls)
		}
	})
	t.Run("require_complete_retries_once", func(t *testing.T) {
		source := &sequenceDeltaSource{samples: []core.DeltaSample{
			deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main"),
			deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main"),
		}}
		barriers := []core.CoherenceBarrier{{DaemonIncarnation: "d", Epoch: 1}, {DaemonIncarnation: "d", Epoch: 2}, {DaemonIncarnation: "d", Epoch: 2}, {DaemonIncarnation: "d", Epoch: 2}}
		limits := core.DeltaLimits{RequireComplete: true}
		got := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, &sequenceDeltaCoherence{barriers: barriers}).Sample(context.Background(), workspace.ID, limits)
		if got.Completeness != core.SelectionComplete || !got.CacheEligible || source.calls != 2 {
			t.Fatalf("sample=%#v calls=%d", got, source.calls)
		}
	})
}

func modifiedDelta(path string) core.ChangeRecord {
	return core.ChangeRecord{PathTransition: core.PathModified, NewPath: path, SourceTransition: core.SourceBytesChanged, VCSTransition: core.VCSOther}
}

func deltaSourceSample(workspace core.Workspace, head, ref string, changes ...core.ChangeRecord) core.DeltaSample {
	return core.DeltaSample{SchemaVersion: core.DeltaSampleSchemaVersion, RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID, Freshness: core.SampleFreshlySampled, Completeness: core.SelectionComplete, ObservedAt: time.Now().UTC(), Head: head, Ref: ref, Changes: changes, RecordsObserved: len(changes)}
}

func deltaSamplerWorkspace() core.Workspace {
	now := time.Now().UTC()
	return core.Workspace{SchemaVersion: core.SchemaVersion, ID: core.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: core.RepositoryID("repo_01K00000000000000000000000"), Label: "delta", Root: "/repo", GitDir: "/repo/.git", Branch: "main", CreatedAt: now, LastSeenAt: now}
}

type sequenceDeltaSource struct {
	samples []core.DeltaSample
	calls   int
}

func (s *sequenceDeltaSource) SampleDelta(context.Context, core.Workspace, core.DeltaLimits) core.DeltaSample {
	index := s.calls
	s.calls++
	if index >= len(s.samples) {
		return s.samples[len(s.samples)-1]
	}
	return s.samples[index]
}

type sequenceDeltaCoherence struct {
	barriers []core.CoherenceBarrier
	calls    int
}

func (s *sequenceDeltaCoherence) CaptureBarrier() core.CoherenceBarrier {
	index := s.calls
	s.calls++
	if index >= len(s.barriers) {
		return s.barriers[len(s.barriers)-1]
	}
	return s.barriers[index]
}

func repeatedBarrier(count int, epoch uint64, active int) []core.CoherenceBarrier {
	out := make([]core.CoherenceBarrier, count)
	for i := range out {
		out[i] = core.CoherenceBarrier{DaemonIncarnation: "d", Epoch: epoch, ActiveManagedShellOperations: active}
	}
	return out
}

type deltaSamplerRegistry struct{ workspace core.Workspace }

func (r *deltaSamplerRegistry) SaveRepository(context.Context, core.Repository) error { return nil }
func (r *deltaSamplerRegistry) SaveWorkspace(context.Context, core.Workspace) error   { return nil }
func (r *deltaSamplerRegistry) ListRepositories(context.Context) ([]core.Repository, error) {
	return nil, nil
}
func (r *deltaSamplerRegistry) ListWorkspaces(context.Context) ([]core.Workspace, error) {
	return []core.Workspace{r.workspace}, nil
}
func (r *deltaSamplerRegistry) DeleteWorkspace(context.Context, core.WorkspaceID) error { return nil }

func TestDeltaSamplerKnownIncompleteRepositoryModeDowngradesSelection(t *testing.T) {
	workspace := deltaSamplerWorkspace()
	sample := deltaSourceSample(workspace, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main")
	sample.SelectionModes = core.SelectionModeQuality{AssumeUnchanged: core.ModePresent}
	source := &sequenceDeltaSource{samples: []core.DeltaSample{sample}}
	coherence := &sequenceDeltaCoherence{barriers: repeatedBarrier(2, 1, 0)}
	got := NewDeltaSampler(&deltaSamplerRegistry{workspace: workspace}, source, coherence).Sample(context.Background(), workspace.ID, core.DeltaLimits{})
	if got.Completeness != core.SelectionPartial || got.CacheEligible || got.DiagnosticCode != "repository_mode_potentially_incomplete" {
		t.Fatalf("sample=%#v", got)
	}
}
