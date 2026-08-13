// Package bridge implements stateless local-shell forwarding.
package bridge

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/app/daemon"
)

type DaemonClient interface {
	Forward(context.Context, Request) (Response, error)
}
type Request struct {
	Action string
	Start  daemon.StartRequest
	Poll   daemon.PollRequest
	Write  daemon.WriteRequest
	Kill   daemon.KillRequest
}
type Response struct {
	View      daemon.View
	Code      string
	Message   string
	Retryable bool
}
