// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/app/daemon"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type DaemonClient interface {
	Forward(context.Context, Request) (Response, error)
}
type Request struct {
	ProtocolVersion int
	Action          string
	WorkspaceID     string
	ActivityID      string
	Start           daemon.StartRequest
	Poll            daemon.PollRequest
	Write           daemon.WriteRequest
	Kill            daemon.KillRequest
}
type Response struct {
	View      daemon.View
	Result    *receipt.Result
	Server    *capability.Catalog
	Project   *project.Inspection
	Workspace *workspace.Workspace
	Activity  *activity.Activity
	Code      string
	Message   string
	Retryable bool
}
