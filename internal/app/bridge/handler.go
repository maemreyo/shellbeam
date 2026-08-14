package bridge

import (
	"context"
	"errors"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type Handler struct{ client DaemonClient }

func New(client DaemonClient) *Handler { return &Handler{client: client} }
func (h *Handler) Handle(ctx context.Context, req Request) (Response, error) {
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
