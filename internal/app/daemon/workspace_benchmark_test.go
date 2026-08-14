package daemon

import (
	"context"
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func BenchmarkWorkspaceStartContextNoTax(b *testing.B) {
	now := time.Now().UTC()
	observer := benchmarkWorkspaceObserver{
		binding: workspace.Binding{
			RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"),
			WorkspaceID:  workspace.WorkspaceID("ws_01K00000000000000000000000"),
		},
		cached: workspace.FastSnapshot{
			SchemaVersion: workspace.SnapshotSchemaVersion,
			RepositoryID:  workspace.RepositoryID("repo_01K00000000000000000000000"),
			WorkspaceID:   workspace.WorkspaceID("ws_01K00000000000000000000000"),
			Generation:    "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Quality:       workspace.QualityCached,
			ObservedAt:    now,
		},
	}
	svc := &Service{observer: observer}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got := svc.captureWorkspace(ctx, "/repo")
		if got.context == nil || got.pre.Generation == "" {
			b.Fatal("cached workspace observation missing")
		}
	}
}

type benchmarkWorkspaceObserver struct {
	binding workspace.Binding
	cached  workspace.FastSnapshot
}

func (o benchmarkWorkspaceObserver) Bind(context.Context, string) workspace.Binding {
	return o.binding
}

func (o benchmarkWorkspaceObserver) ObserveCached(context.Context, string) workspace.FastSnapshot {
	return o.cached
}
