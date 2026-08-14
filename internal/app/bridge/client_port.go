// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type DaemonClient interface {
	Forward(context.Context, Request) (Response, error)
}
type Request struct {
	ProtocolVersion int
	Action          string
	WorkspaceID     string
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
	Code      string
	Message   string
	Retryable bool
}
