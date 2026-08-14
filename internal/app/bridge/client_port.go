// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
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
	ProtocolVersion   int
	Action            string
	WorkspaceID       string
	ActivityID        string
	Start             daemon.StartRequest
	Poll              daemon.PollRequest
	Write             daemon.WriteRequest
	Kill              daemon.KillRequest
	EventInspect      observationapp.InspectRequest
	StructuredInspect structuredapp.InspectRequest
}
type Response struct {
	View       daemon.View
	Result     *receipt.Result
	Server     *capability.Catalog
	Project    *project.Inspection
	Workspace  *workspace.Workspace
	Activity   *activity.Activity
	Events     *observationapp.InspectResult
	Structured *structuredapp.InspectResult
	Code       string
	Message    string
	Retryable  bool
}
