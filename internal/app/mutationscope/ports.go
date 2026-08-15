package mutationscope

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Store interface {
	ListWorkspaces(context.Context) ([]workspace.Workspace, error)
	LoadMutationScope(context.Context, string) (core.Scope, bool, error)
	ListMutationScopes(context.Context, string, workspace.WorkspaceID) ([]core.Scope, error)
	LoadMutationReceipt(context.Context, string) (core.MutationReceipt, bool, error)
	CommitMutationScopeSet(context.Context, core.Scope, core.ScopeIdentity, core.MutationReceipt) error
	CommitMutationScopeRelease(context.Context, string, core.MutationReceipt) error
}

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type SetRequest struct {
	MutationID  string
	ScopeID     string
	ActivityID  string
	WorkspaceID workspace.WorkspaceID
	Mode        core.Mode
	Paths       []string
	TTLMS       int64
}

type ReleaseRequest struct {
	MutationID string
	ScopeID    string
}
type InspectRequest struct {
	WorkspaceID workspace.WorkspaceID
	ActivityID  string
}

type MutationResult struct {
	Receipt             core.MutationReceipt
	Scope               *core.Scope
	Replayed            bool
	CurrentRevision     bool
	Advisories          []core.Advisory
	AdvisoryCount       int
	AdvisoryLimit       int
	AdvisoriesTruncated bool
}
