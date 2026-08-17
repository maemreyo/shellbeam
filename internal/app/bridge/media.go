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
	identity, err := client.Forward(ctx, Request{ProtocolVersion: 2, Action: "inspect.server"})
	if err != nil {
		return nil, err
	}
	if identity.Code != "" {
		return nil, failure.New(failure.Code(identity.Code), nil, nil)
	}
	if identity.Server == nil {
		return nil, failure.New(failure.InvalidDaemonResponse, nil, errors.New("server catalog missing"))
	}
	effective := identity.Server.Clone()
	if effective.Features == nil {
		effective.Features = map[capability.Feature]capability.Availability{}
	}
	effective.Media = nil
	effective.Features[capability.FeatureRichLocalMedia] = capability.Unavailable
	h := &Handler{client: client, effective: effective, hasEffective: true}

	declared := consumer.Clone()
	negotiation, err := client.Forward(ctx, Request{ProtocolVersion: 2, Action: "capabilities.negotiate", ConsumerMedia: &declared})
	if err != nil || negotiation.Code != "" {
		return h, nil
	}
	if negotiation.NegotiatedMedia == nil || !validNegotiatedMedia(consumer, *negotiation.NegotiatedMedia) {
		return nil, failure.New(failure.InvalidDaemonResponse, nil, errors.New("invalid media negotiation"))
	}
	h.consumer = consumer.Clone()
	negotiated := *negotiation.NegotiatedMedia
	negotiated.Contract = negotiated.Contract.Clone()
	h.negotiated = &negotiated
	support := negotiated.Contract.Clone()
	h.effective.Media = &support
	h.effective.Features[capability.FeatureRichLocalMedia] = capability.Available
	return h, nil
}

func validNegotiatedMedia(consumer capability.MediaSupport, got capability.NegotiatedMedia) bool {
	expected, ok := capability.NegotiateMedia(consumer, got.Contract)
	return ok && expected.Fingerprint == got.Fingerprint && reflect.DeepEqual(expected.Contract, got.Contract)
}

func (h *Handler) EffectiveCatalog() capability.Catalog {
	if h == nil || !h.hasEffective {
		return capability.Baseline(capability.Limits{})
	}
	return h.effective.Clone()
}

func (h *Handler) handleMedia(ctx context.Context, req Request) (Response, error) {
	if !h.mediaAvailable() || req.Media == nil {
		return publicFailureResponse(failure.FeatureUnavailable), nil
	}
	expected, err := expectedMediaAddress(*req.Media)
	if err != nil {
		return publicFailureResponse(failure.InvalidInput), nil
	}
	forward := req
	consumer := h.consumer.Clone()
	mediaReq := *req.Media
	forward.ConsumerMedia = &consumer
	forward.MediaContractFingerprint = h.negotiated.Fingerprint
	forward.Media = &mediaReq
	response, err := h.client.Forward(ctx, forward)
	if err != nil {
		return Response{}, err
	}
	if response.Code != "" {
		return normalizeMediaFailureResponse(response), nil
	}
	if response.Media == nil || validateDaemonMedia(*response.Media, expected, *h.effective.Media) != nil {
		return publicFailureResponse(failure.InvalidDaemonResponse), nil
	}
	copyResult := *response.Media
	copyResult.Data = append([]byte(nil), response.Media.Data...)
	response.Media = &copyResult
	return response, nil
}

func (h *Handler) mediaAvailable() bool {
	return h != nil && h.hasEffective && h.negotiated != nil && h.effective.Media != nil && h.effective.Features[capability.FeatureRichLocalMedia] == capability.Available
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
