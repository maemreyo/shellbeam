package bridge

import (
	"context"
	"errors"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type Handler struct {
	mu           sync.RWMutex
	client       DaemonClient
	effective    capability.Catalog
	hasEffective bool
	consumer     capability.MediaSupport
	hasConsumer  bool
	negotiated   *capability.NegotiatedMedia
}

func New(client DaemonClient) *Handler { return &Handler{client: client} }

// CatalogRefreshEnabled is true only for the negotiated production bridge.
// Plain New handlers retain the static compatibility behavior used by legacy
// callers and tests.
func (h *Handler) CatalogRefreshEnabled() bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.hasEffective && h.hasConsumer
}
func (h *Handler) Handle(ctx context.Context, req Request) (Response, error) {
	if req.ProtocolVersion == 2 && req.Action == "inspect.server" && h.hasEffective {
		catalog := h.EffectiveCatalog()
		return Response{Server: &catalog}, nil
	}
	if req.ProtocolVersion == 2 && req.Action == "read_media" {
		return h.handleMedia(ctx, req)
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
