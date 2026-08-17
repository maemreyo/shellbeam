package ipc

import (
	"context"
	"encoding/hex"
	"errors"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type MediaActions interface {
	ReadMedia(context.Context, app.MediaRequest) (media.Result, error)
	MediaSupport() capability.MediaSupport
}

func validateMediaRequestV2(v RequestV2) error {
	if v.ConsumerMedia == nil || !validConsumerMedia(*v.ConsumerMedia) {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	if v.Action == "capabilities.negotiate" {
		if v.Media != nil || v.MediaContractFingerprint != "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "media"}, nil)
		}
		return nil
	}
	if v.Media == nil || !validMediaFingerprint(v.MediaContractFingerprint) {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	if err := validateDaemonMediaRequest(*v.Media); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "media"}, err)
	}
	return nil
}

func validConsumerMedia(s capability.MediaSupport) bool {
	if s.SchemaVersion != 1 || len(s.Kinds) == 0 || len(s.Kinds) > 4 || len(s.MIMETypes) == 0 || len(s.MIMETypes) > 8 {
		return false
	}
	if s.MaxImageBytes <= 0 || s.MaxWidth <= 0 || s.MaxHeight <= 0 || s.MaxPixels <= 0 {
		return false
	}
	for _, kind := range s.Kinds {
		if kind == "image" {
			return true
		}
	}
	return false
}

func validMediaFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateDaemonMediaRequest(req app.MediaRequest) error {
	if _, err := media.ParseLogicalPath(req.Path); err != nil {
		return err
	}
	switch {
	case req.WorkspaceID != "" && req.CWD == "":
		_, err := workspace.ParseWorkspaceID(req.WorkspaceID)
		return err
	case req.WorkspaceID == "" && req.CWD != "":
		return media.ValidateCWD(req.CWD)
	default:
		return errors.New("media request requires exactly one base")
	}
}

func dispatchMediaV2(ctx context.Context, req RequestV2, resp *ResponseV2, raw Actions) error {
	actions, ok := raw.(MediaActions)
	if !ok || req.ConsumerMedia == nil {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	negotiated, ok := capability.NegotiateMedia(*req.ConsumerMedia, actions.MediaSupport())
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	if req.Action == "capabilities.negotiate" {
		resp.NegotiatedMedia = &negotiated
		return nil
	}
	if req.Media == nil || req.MediaContractFingerprint != negotiated.Fingerprint {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	result, err := actions.ReadMedia(ctx, *req.Media)
	if err != nil {
		return err
	}
	resp.Media = &result
	return nil
}

func applyBridgeMediaV2(req *RequestV2, in bridgeMediaRequest) {
	if in.ConsumerMedia != nil {
		consumer := in.ConsumerMedia.Clone()
		req.ConsumerMedia = &consumer
	}
	req.MediaContractFingerprint = in.MediaContractFingerprint
	if in.Media != nil {
		mediaReq := *in.Media
		req.Media = &mediaReq
	}
}

type bridgeMediaRequest struct {
	ConsumerMedia            *capability.MediaSupport
	MediaContractFingerprint string
	Media                    *app.MediaRequest
}

func validateMediaResponseEnvelopeV2(req RequestV2, out ResponseV2) error {
	if out.IPVersion != ipcV2 || out.Kind != "response" || out.RequestID != req.RequestID || out.Action != req.Action {
		return failure.New(failure.InvalidDaemonResponse, nil, errors.New("media response envelope mismatch"))
	}
	if out.OK {
		switch req.Action {
		case "capabilities.negotiate":
			if out.NegotiatedMedia == nil || out.Media != nil || out.Error != nil {
				return failure.New(failure.InvalidDaemonResponse, nil, errors.New("invalid negotiation response"))
			}
		case "read_media":
			if out.Media == nil || out.NegotiatedMedia != nil || out.Error != nil {
				return failure.New(failure.InvalidDaemonResponse, nil, errors.New("invalid media response"))
			}
		}
		return nil
	}
	if out.Error == nil || out.Media != nil || out.NegotiatedMedia != nil {
		return failure.New(failure.InvalidDaemonResponse, nil, errors.New("invalid media failure response"))
	}
	return nil
}
