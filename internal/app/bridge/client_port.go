// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
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
	CodeQuery         *codeintel.Query
	Start             daemon.StartRequest
	Poll              daemon.PollRequest
	Write             daemon.WriteRequest
	Kill              daemon.KillRequest
	EventInspect      observationapp.InspectRequest
	StructuredInspect structuredapp.InspectRequest
	TelemetryInspect  telemetryapp.InspectRequest
	ReproCreate       reprocore.CreateRequest
	ReproID           string
}
type Response struct {
	View       daemon.View
	Result     *receipt.Result
	Server     *capability.Catalog
	Project    *project.Inspection
	Readiness  *project.Readiness
	Workspace  *workspace.Workspace
	Activity   *activity.Activity
	Events     *observationapp.InspectResult
	Structured *structuredapp.InspectResult
	Telemetry  *telemetryapp.InspectResult
	Capsule    *reprocore.Capsule
	Repro      *reproapp.InspectResult
	CodeResult *codeintel.Result
	Code       string
	Message    string
	Retryable  bool
}
