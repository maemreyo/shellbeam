package browserbridge

import (
	"context"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

// DaemonReader is the typed, read-only port used by Browser Bridge plans.
//
// The application layer never sees IPC wire requests. Each method represents
// exactly one read capability and exposes only the selectors that the plan is
// allowed to derive, so callers cannot smuggle an action, command, or session
// selector through this port.
type DaemonReader interface {
	Activity(ctx context.Context, activityID string) (*activitycore.Activity, bool, error)
	Sessions(ctx context.Context, activityID string, maxRecords int) (*persistent.InspectPage, bool, error)
	Events(ctx context.Context, target observationcore.Target, afterCursor string, maxEvents int) (*observationapp.InspectResult, bool, error)
	Verification(ctx context.Context, workspaceID, activityID string) (*verificationapp.Inspection, bool, error)
	Structured(ctx context.Context, operationID string, testStatus structuredcore.TestStatus, maxRecords int) (*structuredapp.InspectResult, bool, error)
}

type Planner struct{ reader DaemonReader }

func NewPlanner(reader DaemonReader) *Planner { return &Planner{reader: reader} }
