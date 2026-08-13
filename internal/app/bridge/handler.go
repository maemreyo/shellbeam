package bridge

import "context"

type Handler struct{ client DaemonClient }

func New(client DaemonClient) *Handler { return &Handler{client: client} }
func (h *Handler) Handle(ctx context.Context, req Request) (Response, error) {
	return h.client.Forward(ctx, req)
}
