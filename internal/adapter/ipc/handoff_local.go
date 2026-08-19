package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const (
	handoffLocalVersion         = 1
	maxHandoffLocalRequestBytes = 16 << 10
)

type HandoffLocalAction string

const (
	HandoffLocalBootstrap HandoffLocalAction = "bootstrap"
	HandoffLocalBind      HandoffLocalAction = "bind"
	HandoffLocalControl   HandoffLocalAction = "control"
)

type HandoffLocalRequest struct {
	LocalVersion   int                      `json:"local_version"`
	Kind           string                   `json:"kind"`
	RequestID      string                   `json:"request_id"`
	Action         HandoffLocalAction       `json:"action"`
	HandoffID      string                   `json:"handoff_id"`
	ClientRef      string                   `json:"client_ref,omitempty"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch,omitempty"`
	ControlID      string                   `json:"control_id,omitempty"`
	ControlKind    handoff.HumanControlKind `json:"control_kind,omitempty"`
}

type HandoffLocalResponse struct {
	LocalVersion int                        `json:"local_version"`
	Kind         string                     `json:"kind"`
	RequestID    string                     `json:"request_id"`
	Action       HandoffLocalAction         `json:"action"`
	OK           bool                       `json:"ok"`
	Bootstrap    *handoffapp.LocalBootstrap `json:"bootstrap,omitempty"`
	State        *handoff.State             `json:"state,omitempty"`
	Control      *handoffapp.ControlResult  `json:"control,omitempty"`
	Error        *Error                     `json:"error,omitempty"`
}

type HandoffLocalActions interface {
	BootstrapLocalHuman(context.Context, string) (handoffapp.LocalBootstrap, error)
	BindLocalHuman(context.Context, string, delegatedapp.ProviderClientRef) (handoff.State, error)
	HandoffHumanControl(context.Context, handoff.ControlSignal) (handoffapp.ControlResult, error)
}

func validateHandoffLocalRequest(req HandoffLocalRequest) error {
	if req.LocalVersion != handoffLocalVersion || req.Kind != "request" || !safeLocalOpaque(req.RequestID, 128) || !safeLocalOpaque(req.HandoffID, 128) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "local_handoff"}, nil)
	}
	switch req.Action {
	case HandoffLocalBootstrap:
		if req.ClientRef != "" || req.AuthorityEpoch != 0 || req.ControlID != "" || req.ControlKind != "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "local_handoff_bootstrap"}, nil)
		}
	case HandoffLocalBind:
		client := delegatedapp.ProviderClientRef{Ref: req.ClientRef}
		if client.Validate() != nil || req.AuthorityEpoch != 0 || req.ControlID != "" || req.ControlKind != "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "local_handoff_bind"}, nil)
		}
	case HandoffLocalControl:
		if req.ClientRef != "" {
			return failure.New(failure.InvalidInput, map[string]string{"field": "local_handoff_control"}, nil)
		}
		sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: req.AuthorityEpoch, ControlID: req.ControlID, Kind: req.ControlKind}
		if err := sig.Validate(); err != nil {
			return err
		}
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "local_handoff_action"}, nil)
	}
	return nil
}

func decodeHandoffLocal(r io.Reader) (HandoffLocalRequest, error) {
	limited := io.LimitReader(r, maxHandoffLocalRequestBytes+1)
	data, err := io.ReadAll(limited)
	if err == nil && len(data) > maxHandoffLocalRequestBytes {
		err = fmt.Errorf("local handoff request too large")
	}
	if err != nil {
		return HandoffLocalRequest{}, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_json"}, err)
	}
	var req HandoffLocalRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_request"}, err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return req, failure.New(failure.InvalidInput, map[string]string{"reason": "invalid_request"}, err)
	}
	return req, validateHandoffLocalRequest(req)
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("trailing json value")
}

func (s *Server) handleHandoffLocal(w http.ResponseWriter, r *http.Request) {
	req, err := decodeHandoffLocal(r.Body)
	resp := HandoffLocalResponse{LocalVersion: handoffLocalVersion, Kind: "response", RequestID: req.RequestID, Action: req.Action}
	if err == nil {
		err = s.awaitReady(r.Context())
	}
	if err == nil {
		actions, ok := s.actions.(HandoffLocalActions)
		if !ok {
			err = failure.New(failure.FeatureUnavailable, map[string]string{"feature": "local_handoff"}, nil)
		} else {
			switch req.Action {
			case HandoffLocalBootstrap:
				v, callErr := actions.BootstrapLocalHuman(r.Context(), req.HandoffID)
				err = callErr
				if callErr == nil {
					resp.Bootstrap = &v
				}
			case HandoffLocalBind:
				v, callErr := actions.BindLocalHuman(r.Context(), req.HandoffID, delegatedapp.ProviderClientRef{Ref: req.ClientRef})
				err = callErr
				if callErr == nil {
					resp.State = &v
				}
			case HandoffLocalControl:
				sig := handoff.ControlSignal{HandoffID: req.HandoffID, AuthorityEpoch: req.AuthorityEpoch, ControlID: req.ControlID, Kind: req.ControlKind}
				v, callErr := actions.HandoffHumanControl(r.Context(), sig)
				err = callErr
				if callErr == nil {
					resp.Control = &v
				}
			}
		}
	}
	resp.OK = err == nil
	if err != nil {
		resp.Bootstrap, resp.State, resp.Control, resp.Error = nil, nil, nil, errorEnvelope(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (c *Client) CallHandoffLocal(ctx context.Context, req HandoffLocalRequest) (HandoffLocalResponse, error) {
	var out HandoffLocalResponse
	if err := validateHandoffLocalRequest(req); err != nil {
		return out, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://shellbeam/local/handoff", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(hreq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("ipc status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	if out.LocalVersion != handoffLocalVersion || out.Kind != "response" || out.RequestID != req.RequestID || out.Action != req.Action {
		return HandoffLocalResponse{}, failure.New(failure.InvalidDaemonResponse, nil, nil)
	}
	if out.Error != nil {
		return out, failure.New(failure.Code(out.Error.Code), out.Error.Details, nil)
	}
	if !out.OK {
		return out, failure.New(failure.InvalidDaemonResponse, nil, nil)
	}
	return out, nil
}

func safeLocalOpaque(v string, max int) bool {
	if len(v) < 1 || len(v) > max {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
