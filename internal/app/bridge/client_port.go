// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type DaemonClient interface {
	Forward(context.Context, Request) (Response, error)
}
type Request struct {
	ProtocolVersion    int
	Action             string
	WorkspaceID        string
	ActivityID         string
	CodeQuery          *codeintel.Query
	Start              daemon.StartRequest
	Poll               daemon.PollRequest
	Write              daemon.WriteRequest
	Kill               daemon.KillRequest
	EventInspect       observationapp.InspectRequest
	EvidenceInspect    evidenceapp.InspectRequest
	EnvironmentInspect environmentapp.InspectRequest
	ProcessInspect     processapp.InspectRequest
	StructuredInspect  structuredapp.InspectRequest
	TelemetryInspect   telemetryapp.InspectRequest
	ReproCreate        reprocore.CreateRequest
	ReproID            string
	OutputRead         outputview.Request
}
type Response struct {
	View        daemon.View
	Result      *receipt.Result
	Server      *capability.Catalog
	Project     *project.Inspection
	Readiness   *project.Readiness
	Workspace   *workspace.Workspace
	Activity    *activity.Activity
	Events      *observationapp.InspectResult
	Evidence    *evidenceapp.InspectResult
	Environment *environmentcore.Snapshot
	Process     *processcore.Observation
	Structured  *structuredapp.InspectResult
	Telemetry   *telemetryapp.InspectResult
	Capsule     *reprocore.Capsule
	Repro       *reproapp.InspectResult
	CodeResult  *codeintel.Result
	OutputView  *outputview.Result
	Code        string
	Message     string
	Retryable   bool
}
