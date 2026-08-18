package bridge

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"reflect"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	_ "golang.org/x/image/webp"
)

func NewNegotiated(ctx context.Context, client DaemonClient, consumer capability.MediaSupport) (*Handler, error) {
	h := &Handler{client: client, consumer: consumer.Clone(), hasConsumer: true}
	if _, err := h.RefreshEffectiveCatalog(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

// RefreshEffectiveCatalog re-reads daemon capabilities on demand. It performs
// no background polling. Rich-local-media is intentionally absent from the
// legacy-compatible inspect.server catalog, so each actual refresh repeats the
// private consumer negotiation. The MCP adapter bounds that refresh frequency.
func (h *Handler) RefreshEffectiveCatalog(ctx context.Context) (bool, error) {
	if h == nil || h.client == nil {
		return false, failure.New(failure.InvalidDaemonResponse, nil, errors.New("daemon client missing"))
	}
	identity, err := h.client.Forward(ctx, Request{ProtocolVersion: 2, Action: "inspect.server"})
	if err != nil {
		return false, err
	}
	if identity.Code != "" {
		return false, failure.New(failure.Code(identity.Code), nil, nil)
	}
	if identity.Server == nil {
		return false, failure.New(failure.InvalidDaemonResponse, nil, errors.New("server catalog missing"))
	}

	effective := identity.Server.Clone()
	if effective.Features == nil {
		effective.Features = map[capability.Feature]capability.Availability{}
	}
	// inspect.server is deliberately legacy-compatible and therefore never
	// grants media capability authority. Only the private negotiation below can.
	effective.Media = nil
	effective.Features[capability.FeatureRichLocalMedia] = capability.Unavailable

	h.mu.RLock()
	hasConsumer := h.hasConsumer
	consumer := h.consumer.Clone()
	h.mu.RUnlock()

	var negotiated *capability.NegotiatedMedia
	if hasConsumer {
		declared := consumer.Clone()
		response, negotiateErr := h.client.Forward(ctx, Request{ProtocolVersion: 2, Action: "capabilities.negotiate", ConsumerMedia: &declared})
		if negotiateErr == nil && response.Code == "" {
			if response.NegotiatedMedia == nil || !validNegotiatedMedia(consumer, *response.NegotiatedMedia) {
				return false, failure.New(failure.InvalidDaemonResponse, nil, errors.New("invalid media negotiation"))
			}
			copy := *response.NegotiatedMedia
			copy.Contract = copy.Contract.Clone()
			negotiated = &copy
		}
	}
	if negotiated != nil {
		support := negotiated.Contract.Clone()
		effective.Media = &support
		effective.Features[capability.FeatureRichLocalMedia] = capability.Available
	}

	h.mu.Lock()
	changed := !h.hasEffective || !reflect.DeepEqual(h.effective, effective) || !reflect.DeepEqual(h.negotiated, negotiated)
	h.effective = effective
	h.hasEffective = true
	h.negotiated = cloneNegotiatedMediaPtr(negotiated)
	h.mu.Unlock()
	return changed, nil
}

func cloneNegotiatedMediaPtr(value *capability.NegotiatedMedia) *capability.NegotiatedMedia {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Contract = value.Contract.Clone()
	return &copy
}

func validNegotiatedMedia(consumer capability.MediaSupport, got capability.NegotiatedMedia) bool {
	expected, ok := capability.NegotiateMedia(consumer, got.Contract)
	return ok && expected.Fingerprint == got.Fingerprint && reflect.DeepEqual(expected.Contract, got.Contract)
}

func (h *Handler) EffectiveCatalog() capability.Catalog {
	if h == nil {
		return capability.Baseline(capability.Limits{})
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.hasEffective {
		return capability.Baseline(capability.Limits{})
	}
	return h.effective.Clone()
}

func (h *Handler) handleMedia(ctx context.Context, req Request) (Response, error) {
	consumer, negotiated, support, ok := h.mediaSnapshot()
	if !ok || req.Media == nil {
		return publicFailureResponse(failure.FeatureUnavailable), nil
	}
	expected, err := expectedMediaAddress(*req.Media)
	if err != nil {
		return publicFailureResponse(failure.InvalidInput), nil
	}
	forward := req
	mediaReq := *req.Media
	forward.ConsumerMedia = &consumer
	forward.MediaContractFingerprint = negotiated.Fingerprint
	forward.Media = &mediaReq
	response, err := h.client.Forward(ctx, forward)
	if err != nil {
		return Response{}, err
	}
	if response.Code != "" {
		return normalizeMediaFailureResponse(response), nil
	}
	if response.Media == nil || validateDaemonMedia(*response.Media, expected, support) != nil {
		return publicFailureResponse(failure.InvalidDaemonResponse), nil
	}
	copyResult := *response.Media
	copyResult.Data = append([]byte(nil), response.Media.Data...)
	response.Media = &copyResult
	return response, nil
}

func (h *Handler) mediaAvailable() bool {
	_, _, _, ok := h.mediaSnapshot()
	return ok
}

func (h *Handler) mediaSnapshot() (capability.MediaSupport, capability.NegotiatedMedia, capability.MediaSupport, bool) {
	if h == nil {
		return capability.MediaSupport{}, capability.NegotiatedMedia{}, capability.MediaSupport{}, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.hasEffective || h.negotiated == nil || h.effective.Media == nil || h.effective.Features[capability.FeatureRichLocalMedia] != capability.Available {
		return capability.MediaSupport{}, capability.NegotiatedMedia{}, capability.MediaSupport{}, false
	}
	consumer := h.consumer.Clone()
	negotiated := *h.negotiated
	negotiated.Contract = h.negotiated.Contract.Clone()
	support := h.effective.Media.Clone()
	return consumer, negotiated, support, true
}

func expectedMediaAddress(req daemonapp.MediaRequest) (media.DisplayAddress, error) {
	if _, err := media.ParseLogicalPath(req.Path); err != nil {
		return media.DisplayAddress{}, err
	}
	switch {
	case req.WorkspaceID != "" && req.CWD == "":
		if _, err := workspace.ParseWorkspaceID(req.WorkspaceID); err != nil {
			return media.DisplayAddress{}, err
		}
		return media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: req.WorkspaceID, Path: req.Path}, nil
	case req.WorkspaceID == "" && req.CWD != "":
		if err := media.ValidateCWD(req.CWD); err != nil {
			return media.DisplayAddress{}, err
		}
		return media.DisplayAddress{AddressKind: media.AddressCWD, CWD: req.CWD, Path: req.Path}, nil
	default:
		return media.DisplayAddress{}, errors.New("media request requires exactly one base")
	}
}

func publicFailureResponse(code failure.Code) Response {
	public := failure.Public(failure.New(code, nil, nil))
	return Response{Code: string(public.Code), Message: public.Message, Retryable: public.Retryable}
}

func normalizeMediaFailureResponse(response Response) Response {
	public := failure.Public(errors.New(response.Code))
	response.Code = string(public.Code)
	response.Message = public.Message
	response.Retryable = public.Retryable
	response.Media = nil
	return response
}

func validateDaemonMedia(result media.Result, expected media.DisplayAddress, support capability.MediaSupport) error {
	if result.SchemaVersion != 1 || result.Kind != "media" || result.DisplayAddress != expected || result.DisplayAddress.Validate() != nil {
		return errors.New("media identity mismatch")
	}
	if result.ByteSize != len(result.Data) || result.ByteSize <= 0 || result.ByteSize > support.MaxImageBytes || result.ByteSize > media.MaxImageBytes {
		return errors.New("media byte size invalid")
	}
	if result.Width <= 0 || result.Height <= 0 || result.Width > support.MaxWidth || result.Height > support.MaxHeight || int64(result.Width)*int64(result.Height) > support.MaxPixels {
		return errors.New("media dimensions invalid")
	}
	if !containsString(support.MIMETypes, result.MIMEType) {
		return errors.New("media MIME unavailable")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || cfg.Width != result.Width || cfg.Height != result.Height || format != result.Format {
		return errors.New("media bytes invalid")
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if mime == "" || mime != result.MIMEType {
		return errors.New("media MIME mismatch")
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
