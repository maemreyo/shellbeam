package activity

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/activity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Registry interface {
	LoadActivity(context.Context, core.ID) (core.Activity, bool, error)
	SaveActivity(context.Context, core.Activity) error
}

type BaselineSource interface {
	CaptureBaseline(context.Context, workspace.WorkspaceID, string) core.Observation
}
