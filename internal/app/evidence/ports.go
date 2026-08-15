package evidence

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Repository interface {
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	LoadSession(context.Context, operation.SessionID) (session.Snapshot, error)
	ListWorkspaces(context.Context) ([]workspace.Workspace, error)
	PutEvidenceRecord(context.Context, core.Record) (bool, error)
}

type ArtifactObserver interface {
	Observe(context.Context, string, []project.Output) ([]core.ArtifactObservation, error)
}
