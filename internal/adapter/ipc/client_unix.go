//go:build linux || darwin

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
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
