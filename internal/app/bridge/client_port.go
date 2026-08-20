// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	"github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	mutationscopeapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/media"
	mutationscopecore "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type DaemonClient interface {
	Forward(context.Context, Request) (Response, error)
}
type Request struct {
	ProtocolVersion           int
	Action                    string
	WorkspaceID               string
	ActivityID                string
	CodeQuery                 *codeintel.Query
	Start                     daemon.StartRequest
	Poll                      daemon.PollRequest
	Write                     daemon.WriteRequest
	Kill                      daemon.KillRequest
	EventInspect              observationapp.InspectRequest
	EvidenceInspect           evidenceapp.InspectRequest
	EnvironmentInspect        environmentapp.InspectRequest
	ProcessInspect            processapp.InspectRequest
	MutationScopeSet          mutationscopeapp.SetRequest
	MutationScopeRelease      mutationscopeapp.ReleaseRequest
	MutationScopeInspect      mutationscopeapp.InspectRequest
	StructuredInspect         structuredapp.InspectRequest
	TelemetryInspect          telemetryapp.InspectRequest
	ReproCreate               reprocore.CreateRequest
	ReproID                   string
	OutputRead                outputview.Request
	SessionInspect            persistent.InspectRequest
	ConsumerMedia             *capability.MediaSupport
	MediaContractFingerprint  string
	Media                     *daemon.MediaRequest
	CheckpointCreate          checkpointcore.CreateRequest
	CheckpointRestore         checkpointcore.RestoreRequest
	CheckpointID              string
	InputTraceInspect         inputtraceapp.InspectRequest
	VerificationInspect       verificationapp.InspectRequest
	VerificationPolicyPreview verificationapp.PreviewPolicyRequest
	VerificationActivate      verificationapp.ActivateRequest
	VerificationWaiverSet     verificationapp.SetWaiverRequest
	VerificationWaiverRevoke  verificationapp.RevokeWaiverRequest
	Decision                  *DecisionRequest
}
type Response struct {
	View                      daemon.View
	Result                    *receipt.Result
	Checkpoint                *checkpointcore.Checkpoint
	Restore                   *checkpointcore.RestoreResult
	CheckpointInspection      *checkpointapp.CheckpointInspection
	Server                    *capability.Catalog
	Project                   *project.Inspection
	Readiness                 *project.Readiness
	Workspace                 *workspace.Workspace
	Activity                  *activity.Activity
	Events                    *observationapp.InspectResult
	Evidence                  *evidenceapp.InspectResult
	Environment               *environmentcore.Snapshot
	Process                   *processcore.Observation
	Mutation                  *mutationscopeapp.MutationResult
	MutationScopes            *mutationscopecore.InspectResult
	ActivityMutationScopes    *mutationscopecore.InspectResult
	Structured                *structuredapp.InspectResult
	Telemetry                 *telemetryapp.InspectResult
	InputTrace                *inputtraceapp.InspectResult
	Capsule                   *reprocore.Capsule
	Repro                     *reproapp.InspectResult
	CodeResult                *codeintel.Result
	OutputView                *outputview.Result
	Sessions                  *persistent.InspectPage
	NegotiatedMedia           *capability.NegotiatedMedia
	Media                     *media.Result
	Verification              *verificationapp.Inspection
	VerificationPolicyPreview *verificationapp.PolicyPreview
	VerificationActivation    *verificationcore.ActivationWriteResult
	VerificationWaiver        *verificationcore.WaiverWriteResult
	VerificationRevocation    *verificationcore.RevocationWriteResult
	Decision                  *DecisionResponse
	Code                      string
	Message                   string
	Retryable                 bool
	Details                   map[string]string
}
