package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

// DaemonReader is the only way a read plan reaches the daemon.
//
// The interface is intentionally one method wide. Every request a plan builds
// is constructed inside this package from a fixed action string, so no caller
// input can select an action, a command, or a session.
type DaemonReader interface {
	Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error)
}

type Planner struct{ reader DaemonReader }

func NewPlanner(reader DaemonReader) *Planner { return &Planner{reader: reader} }
