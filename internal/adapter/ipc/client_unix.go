//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/jsonstrict"
	"github.com/maemreyo/shellbeam/internal/core/media"
	mutationscopecore "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	"io"
	"net"
	"net/http"
)

type Client struct{ http *http.Client }

func NewClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Client{http: &http.Client{Transport: tr}}
}

func (c *Client) Forward(ctx context.Context, in bridge.Request) (bridge.Response, error) {
	if in.ProtocolVersion >= 2 {
		return c.forwardV2(ctx, in)
	}
	a := Action{Action: in.Action}
	switch in.Action {
	case "start":
		a.OperationID = in.Start.OperationID
		a.Command = in.Start.Command
		a.CWD = in.Start.CWD
		a.TTY = in.Start.TTY
		a.TimeoutMS = in.Start.TimeoutMS
		a.YieldMS = in.Start.YieldMS
		a.MaxOutputBytes = in.Start.MaxOutputBytes
	case "poll":
		a.SessionID = in.Poll.SessionID
		a.Cursor = in.Poll.Cursor
		a.YieldMS = in.Poll.YieldMS
		a.MaxOutputBytes = in.Poll.MaxOutputBytes
	case "write":
		a.SessionID = in.Write.SessionID
		a.InputOffset = in.Write.InputOffset
		a.Chars = in.Write.Chars
		a.EOF = in.Write.EOF
	case "kill":
		a.SessionID = in.Kill.SessionID
		a.KillID = in.Kill.KillID
		a.Signal = in.Kill.Signal
	}
	out, err := c.Call(ctx, Request{IPVersion: 1, RequestID: "bridge", Payload: a})
	if err != nil {
		return bridge.Response{}, err
	}
	if out.Error != nil {
		return bridge.Response{View: out.View, Code: out.Error.Code, Message: out.Error.Message, Retryable: out.Error.Retryable}, nil
	}
	return bridge.Response{View: out.View}, nil
}

func (c *Client) forwardV2(ctx context.Context, in bridge.Request) (bridge.Response, error) {
	req := requestV2FromBridge(in)
	out, err := c.CallV2(ctx, req)
	if err != nil {
		return bridge.Response{}, err
	}
	response := bridge.Response{Result: out.Result, Checkpoint: out.Checkpoint, Restore: out.Restore, CheckpointInspection: out.CheckpointInspection, Server: out.Server, Project: out.Project, Readiness: out.Readiness, Workspace: out.Workspace, Activity: out.Activity, Events: out.Events, Structured: out.Structured, Evidence: out.Evidence, Environment: out.Environment, Process: out.Process, Mutation: out.Mutation, MutationScopes: out.MutationScopes, Telemetry: out.Telemetry, InputTrace: out.InputTrace, Capsule: out.Capsule, Repro: out.Repro, CodeResult: out.Code, OutputView: out.OutputView, Sessions: out.Sessions, NegotiatedMedia: out.NegotiatedMedia, Media: out.Media}
	if out.View != nil {
		response.View = *out.View
	}
	if out.ActiveMutationScopes != nil || out.MutationScopeAdvisories != nil || out.MutationScopesTruncated || out.MutationScopeAdvisoriesTruncated {
		response.ActivityMutationScopes = &mutationscopecore.InspectResult{
			ActiveScopes:    append([]mutationscopecore.Scope(nil), out.ActiveMutationScopes...),
			Advisories:      append([]mutationscopecore.Advisory(nil), out.MutationScopeAdvisories...),
			ScopesTruncated: out.MutationScopesTruncated, AdvisoriesTruncated: out.MutationScopeAdvisoriesTruncated,
		}
	}
	if out.Error != nil {
		response.Code = out.Error.Code
		response.Message = out.Error.Message
		response.Retryable = out.Error.Retryable
		response.Details = cloneStringMapV2(out.Error.Details)
	}
	return response, nil
}

func applyMutationScopeV2(req *RequestV2, in bridge.Request) {
	switch in.Action {
	case "mutation_scope.set":
		v := in.MutationScopeSet
		req.MutationID, req.ScopeID, req.ActivityID, req.WorkspaceID = v.MutationID, v.ScopeID, v.ActivityID, string(v.WorkspaceID)
		req.Mode, req.Paths, req.TTLMS = v.Mode, append([]string(nil), v.Paths...), v.TTLMS
	case "mutation_scope.release":
		req.MutationID, req.ScopeID = in.MutationScopeRelease.MutationID, in.MutationScopeRelease.ScopeID
	case "inspect.mutation_scopes":
		req.WorkspaceID, req.ActivityID = string(in.MutationScopeInspect.WorkspaceID), in.MutationScopeInspect.ActivityID
	}
}

func applyStartV2(req *RequestV2, in bridge.Request) {
	req.OperationID = in.Start.OperationID
	req.ActivityID = in.Start.ActivityID
	req.WorkspaceID = in.Start.WorkspaceID
	req.WorkspaceHint = in.Start.WorkspaceHint
	req.StructuredAdapter = in.Start.StructuredAdapter
	req.ProjectCommandID = in.Start.ProjectCommandID
	req.Params = cloneStringMapV2(in.Start.Params)
	req.Command = in.Start.Command
	req.Argv = append([]string(nil), in.Start.Argv...)
	req.Intent = in.Start.Intent
	req.Evidence = in.Start.Evidence
	req.CWD = in.Start.CWD
	req.TTY = in.Start.TTY
	req.Persistent = in.Start.Persistent
	req.SessionName = in.Start.SessionName
	req.TimeoutMS = in.Start.TimeoutMS
	req.ResourceLimits = in.Start.ResourceLimits.Clone()
	req.Hermetic = in.Start.Hermetic.Clone()
	req.YieldMS = in.Start.YieldMS
	req.MaxOutputBytes = in.Start.MaxOutputBytes
	req.TraceMode = in.Start.TraceMode
}

func applyObservationInspectV2(req *RequestV2, in bridge.Request) {
	if in.Action == "inspect.environment" {
		req.WorkspaceID = in.EnvironmentInspect.WorkspaceID
		req.Freshness = in.EnvironmentInspect.Freshness
		if in.EnvironmentInspect.Execution != nil {
			execution := *in.EnvironmentInspect.Execution
			req.Execution = &execution
		}
		return
	}
	target := in.ProcessInspect.Target
	req.ProcessTarget = &target
	req.IncludePorts = in.ProcessInspect.IncludePorts
}

func applyEvidenceInspectV2(req *RequestV2, in bridge.Request) {
	req.EvidenceID = in.EvidenceInspect.Filter.EvidenceID
	req.OperationID = in.EvidenceInspect.Filter.OperationID
	req.WorkspaceID = in.EvidenceInspect.Filter.WorkspaceID
	req.ProjectCommandID = in.EvidenceInspect.Filter.ProjectCommandID
	req.ActivityID = in.EvidenceInspect.Filter.ActivityID
	req.VerificationKind = in.EvidenceInspect.Filter.VerificationKind
	req.EvidenceResult = in.EvidenceInspect.Filter.Result
	req.RevalidateArtifacts = in.EvidenceInspect.Filter.RevalidateArtifacts
	req.Continuation = in.EvidenceInspect.Continuation
	req.MaxRecords = in.EvidenceInspect.MaxRecords
}

func (c *Client) Call(ctx context.Context, req Request) (Response, error) {
	var out Response
	b, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://shellbeam/v1/local-shell", bytes.NewReader(b))
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
	d := json.NewDecoder(resp.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) CallV2(ctx context.Context, req RequestV2) (ResponseV2, error) {
	var out ResponseV2
	if err := validateRequestV2(req); err != nil {
		return out, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://shellbeam/v2/local-shell", bytes.NewReader(body))
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
	if req.Action == "read_media" || req.Action == "capabilities.negotiate" {
		return decodeMediaResponseV2(resp.Body, req)
	}
	d := json.NewDecoder(resp.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(&out); err != nil {
		return out, err
	}
	if out.IPVersion != ipcV2 || out.Kind != "response" {
		return out, fmt.Errorf("invalid ipc v2 response")
	}
	return out, nil
}

func decodeMediaResponseV2(body io.Reader, req RequestV2) (ResponseV2, error) {
	var out ResponseV2
	limited := io.LimitReader(body, media.MaxOuterResponseBytes+1)
	raw, readErr := io.ReadAll(limited)
	if len(raw) > media.MaxOuterResponseBytes {
		return out, failure.New(failure.InvalidDaemonResponse, nil, fmt.Errorf("media response too large"))
	}
	if readErr != nil {
		return out, failure.New(failure.InvalidDaemonResponse, nil, fmt.Errorf("media response read failed"))
	}
	if err := jsonstrict.Decode(raw, &out); err != nil {
		return ResponseV2{}, failure.New(failure.InvalidDaemonResponse, nil, fmt.Errorf("invalid media response"))
	}
	if err := validateMediaResponseEnvelopeV2(req, out); err != nil {
		return ResponseV2{}, err
	}
	return out, nil
}
