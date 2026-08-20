package bridge

import (
	"context"
	"errors"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type Handler struct {
	client       DaemonClient
	effective    capability.Catalog
	hasEffective bool
	consumer     capability.MediaSupport
	negotiated   *capability.NegotiatedMedia

	terminalAffinityMu sync.RWMutex
	terminalAffinity   *terminalpresentation.BridgeAffinityHint
}

func New(client DaemonClient) *Handler { return &Handler{client: client} }
func (h *Handler) Handle(ctx context.Context, req Request) (Response, error) {
	if req.ProtocolVersion == 2 && req.Action == "inspect.server" && h.hasEffective {
		catalog := h.EffectiveCatalog()
		return Response{Server: &catalog}, nil
	}
	if req.ProtocolVersion == 2 && req.Action == "read_media" {
		return h.handleMedia(ctx, req)
	}
	if req.ProtocolVersion == 2 && req.Action == "handoff.request" {
		req.TerminalAffinity = h.TerminalAffinity()
	} else {
		req.TerminalAffinity = nil
	}
	response, err := h.client.Forward(ctx, req)
	if err != nil || response.Code == "" {
		return response, err
	}
	public := failure.Public(errors.New(response.Code))
	response.Code = string(public.Code)
	response.Message = public.Message
	response.Retryable = public.Retryable
	return response, nil
}
