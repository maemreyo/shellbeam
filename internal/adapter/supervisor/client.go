//go:build linux || darwin

package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type Client struct {
	mu         sync.Mutex
	conn       net.Conn
	reader     *bufio.Reader
	layout     Layout
	capability Capability
	sessionID  string
	generation string
	status     persistentapp.Status
	killSeq    uint64
}

func DialAttachment(ctx context.Context, layout Layout, capability Capability, sessionID, generationID string) (*Client, persistentapp.Status, error) {
	if err := validateLayout(layout); err != nil {
		return nil, persistentapp.Status{}, err
	}
	metadata, err := LoadMetadata(layout)
	if err != nil || metadata.SessionID != sessionID || metadata.GenerationID != generationID {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "identity"}, nil)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", layout.SocketPath)
	if err != nil {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": sessionID, "reason": "connect"}, nil)
	}
	client := &Client{
		conn: conn, reader: bufio.NewReaderSize(conn, MaxFrameBytes+1), layout: layout, capability: capability,
		sessionID: sessionID, generation: generationID,
	}
	challenge, err := NewChallenge()
	if err != nil {
		_ = conn.Close()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorAuthFailed, map[string]string{"session_id": sessionID, "reason": "challenge"}, nil)
	}
	proof, err := Proof(capability, sessionID, generationID, challenge)
	if err != nil {
		_ = conn.Close()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorAuthFailed, map[string]string{"session_id": sessionID, "reason": "proof"}, nil)
	}
	request := Request{ProtocolVersion: ProtocolVersion, Kind: KindHandshake, SessionID: sessionID, GenerationID: generationID, Handshake: &HandshakeRequest{Challenge: challenge, Proof: proof}}
	if err := writeProtocolFrame(conn, request); err != nil {
		_ = conn.Close()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": sessionID, "reason": "handshake_write"}, nil)
	}
	response, err := DecodeResponse(client.reader)
	if err != nil {
		_ = conn.Close()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": sessionID, "reason": "handshake_read"}, nil)
	}
	if response.SessionID != sessionID || response.GenerationID != generationID {
		_ = conn.Close()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "handshake_identity"}, nil)
	}
	if !response.OK || !response.Authenticated || response.Status == nil {
		_ = conn.Close()
		if response.Error != nil {
			return nil, persistentapp.Status{}, responseFailure(response.Error)
		}
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorAuthFailed, map[string]string{"session_id": sessionID, "reason": "handshake"}, nil)
	}
	status := client.applyStatus(response.Status)
	return client, status, nil
}

func (c *Client) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status.PID
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) Write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.writeInputLocked(c.status.NextInputOffset, data, false)
	return err
}

func (c *Client) CloseStdin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.writeInputLocked(c.status.NextInputOffset, nil, true)
	return err
}

func (c *Client) WriteInput(ctx context.Context, offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	if err := ctx.Err(); err != nil {
		return persistentapp.InputResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeInputLocked(offset, data, eof)
}

func (c *Client) writeInputLocked(offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	response, err := c.roundTripLocked(Request{
		ProtocolVersion: ProtocolVersion, Kind: KindWrite, SessionID: c.sessionID, GenerationID: c.generation,
		Write: &WriteRequest{InputOffset: offset, Chars: string(data), EOF: eof},
	})
	if err != nil {
		return persistentapp.InputResult{}, err
	}
	if response.Write == nil {
		return persistentapp.InputResult{}, protocolFailure("write_response")
	}
	result := persistentapp.InputResult{
		AcceptedBytes: response.Write.AcceptedBytes, NextOffset: response.Write.NextOffset,
		Duplicate: response.Write.Duplicate, EOFDelivered: response.Write.EOFDelivered,
	}
	c.status.NextInputOffset = result.NextOffset
	if !result.Duplicate {
		c.status.InputAcceptedBytes += int64(result.AcceptedBytes)
	}
	c.status.StdinClosed = c.status.StdinClosed || result.EOFDelivered
	return result, nil
}

func (c *Client) Signal(name string) receipt.SignalEvidence {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killSeq++
	result, err := c.signalWithIDLocked(fmt.Sprintf("proxy-kill-%d", c.killSeq), name)
	if err != nil {
		return receipt.SignalEvidence{Requested: name}
	}
	return receipt.SignalEvidence{Requested: result.Signal, Attempted: result.Attempted, Succeeded: result.Succeeded}
}

func (c *Client) SignalWithID(ctx context.Context, killID, signalName string) (persistentapp.KillResult, error) {
	if err := ctx.Err(); err != nil {
		return persistentapp.KillResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signalWithIDLocked(killID, signalName)
}

func (c *Client) signalWithIDLocked(killID, signalName string) (persistentapp.KillResult, error) {
	response, err := c.roundTripLocked(Request{
		ProtocolVersion: ProtocolVersion, Kind: KindSignal, SessionID: c.sessionID, GenerationID: c.generation,
		Signal: &SignalRequest{KillID: killID, Signal: signalName},
	})
	if err != nil {
		return persistentapp.KillResult{}, err
	}
	if response.Signal == nil {
		return persistentapp.KillResult{}, protocolFailure("signal_response")
	}
	result := persistentapp.KillResult{
		KillID: response.Signal.KillID, Signal: response.Signal.Signal,
		Attempted: response.Signal.Attempted, Succeeded: response.Signal.Succeeded, Needed: response.Signal.Needed,
	}
	c.status.Signal = receipt.SignalEvidence{Requested: result.Signal, Attempted: result.Attempted, Succeeded: result.Succeeded}
	return result, nil
}

func (c *Client) Wait(ctx context.Context) receipt.ExitEvidence {
	for {
		c.mu.Lock()
		status := c.status
		c.mu.Unlock()
		if status.State.Terminal() {
			return status.Exit
		}
		next, err := c.WaitStatus(ctx, status.Change, MaxWaitMS)
		if err != nil {
			return receipt.ExitEvidence{}
		}
		if next.State.Terminal() {
			return next.Exit
		}
	}
}

func (c *Client) ReadOutput(ctx context.Context, offset int64, maxBytes int) ([]byte, int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, offset, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.roundTripLocked(Request{
		ProtocolVersion: ProtocolVersion, Kind: KindOutput, SessionID: c.sessionID, GenerationID: c.generation,
		Output: &OutputRequest{Offset: offset, MaxBytes: maxBytes},
	})
	if err != nil {
		if !errors.Is(err, failure.SupervisorUnavailable) {
			return nil, offset, 0, err
		}
		spool, _, recoveryErr := openVerifiedTerminalSpool(c.layout, c.capability, c.sessionID, c.generation)
		if recoveryErr != nil {
			return nil, offset, 0, err
		}
		defer spool.Close()
		data, extent, recoveryErr := spool.ReadRange(offset, maxBytes)
		return data, offset + int64(len(data)), extent, recoveryErr
	}
	if response.Output == nil {
		return nil, offset, 0, protocolFailure("output_response")
	}
	return append([]byte(nil), response.Output.Data...), response.Output.NextOffset, response.Output.Extent, nil
}

func (c *Client) AcknowledgeOutput(ctx context.Context, offset int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.roundTripLocked(Request{
		ProtocolVersion: ProtocolVersion, Kind: KindOutputAck, SessionID: c.sessionID, GenerationID: c.generation,
		OutputAck: &OutputAckRequest{Offset: offset},
	})
	if err != nil {
		if !errors.Is(err, failure.SupervisorUnavailable) {
			return err
		}
		spool, _, recoveryErr := openVerifiedTerminalSpool(c.layout, c.capability, c.sessionID, c.generation)
		if recoveryErr != nil {
			return err
		}
		defer spool.Close()
		if recoveryErr = spool.Acknowledge(offset); recoveryErr != nil {
			return recoveryErr
		}
		c.status.OutputAcknowledged = offset
		return nil
	}
	if response.OutputAck == nil || response.OutputAck.Acknowledged != offset {
		return protocolFailure("output_ack_response")
	}
	c.status.OutputAcknowledged = offset
	return nil
}

func (c *Client) Status(ctx context.Context) (persistentapp.Status, error) {
	if err := ctx.Err(); err != nil {
		return persistentapp.Status{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindStatus, SessionID: c.sessionID, GenerationID: c.generation})
	if err != nil {
		return persistentapp.Status{}, err
	}
	if response.Status == nil {
		return persistentapp.Status{}, protocolFailure("status_response")
	}
	return c.applyStatusLocked(response.Status), nil
}

func (c *Client) WaitStatus(ctx context.Context, after uint64, waitMS int) (persistentapp.Status, error) {
	if err := ctx.Err(); err != nil {
		return persistentapp.Status{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.roundTripLocked(Request{
		ProtocolVersion: ProtocolVersion, Kind: KindWait, SessionID: c.sessionID, GenerationID: c.generation,
		Wait: &WaitRequest{AfterChange: after, WaitMS: waitMS},
	})
	if err != nil {
		return persistentapp.Status{}, err
	}
	if response.Status == nil {
		return persistentapp.Status{}, protocolFailure("wait_response")
	}
	return c.applyStatusLocked(response.Status), nil
}

func (c *Client) Terminal(ctx context.Context) (persistentapp.TerminalEvidence, error) {
	if err := ctx.Err(); err != nil {
		return persistentapp.TerminalEvidence{}, err
	}
	record, err := LoadTerminalRecord(c.layout, c.capability, c.sessionID, c.generation)
	if err != nil {
		return persistentapp.TerminalEvidence{}, err
	}
	return persistentapp.TerminalEvidence{
		SessionID: record.SessionID, GenerationID: record.GenerationID, State: record.State, Outcome: record.Outcome,
		Spawn: record.Spawn, Exit: record.Exit, Signal: record.Signal, TimedOut: record.TimedOut,
		OutputBytes: record.OutputBytes, OutputComplete: record.OutputComplete,
		InputAcceptedBytes: record.InputAcceptedBytes, InputDeliveredBytes: record.InputDeliveredBytes,
		StdinClosed: record.StdinClosed, FailureReason: record.FailureReason,
	}, nil
}

func (c *Client) RecoveryState(ctx context.Context) (int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	spool, _, err := openVerifiedTerminalSpool(c.layout, c.capability, c.sessionID, c.generation)
	if err != nil {
		return 0, 0, err
	}
	defer spool.Close()
	return spool.Acknowledged(), spool.Size(), nil
}

func (c *Client) Cleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
	return cleanupVerifiedPrivateState(ctx, c.layout, c.capability, c.sessionID, c.generation)
}

func (c *Client) roundTripLocked(request Request) (Response, error) {
	if c.conn == nil {
		return Response{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": c.sessionID, "reason": "closed"}, nil)
	}
	if err := writeProtocolFrame(c.conn, request); err != nil {
		return Response{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": c.sessionID, "reason": "write"}, nil)
	}
	response, err := DecodeResponse(c.reader)
	if err != nil {
		return Response{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": c.sessionID, "reason": "read"}, nil)
	}
	if response.SessionID != c.sessionID || response.GenerationID != c.generation || response.Kind != request.Kind {
		return Response{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": c.sessionID, "reason": "response_identity"}, nil)
	}
	if !response.OK {
		if response.Error == nil {
			return Response{}, protocolFailure("error_response")
		}
		return Response{}, responseFailure(response.Error)
	}
	return response, nil
}

func (c *Client) applyStatus(status *StatusResponse) persistentapp.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applyStatusLocked(status)
}

func (c *Client) applyStatusLocked(status *StatusResponse) persistentapp.Status {
	result := persistentapp.Status{
		SessionID: c.sessionID, GenerationID: c.generation, State: status.State, Outcome: status.Outcome,
		Change: status.Change, PID: status.PID, OutputBytes: status.OutputBytes, OutputAcknowledged: status.OutputAcknowledged,
		InputAcceptedBytes: status.InputAcceptedBytes, InputDeliveredBytes: status.InputDeliveredBytes,
		NextInputOffset: status.NextInputOffset, StdinClosed: status.StdinClosed,
		Spawn: status.Spawn, Exit: status.Exit, Signal: status.Signal, FailureReason: status.FailureReason,
	}
	c.status = result
	return result
}

func responseFailure(value *ProtocolError) error {
	if value == nil {
		return failure.New(failure.Internal, nil, nil)
	}
	return failure.New(failure.Code(value.Code), value.Details, fmt.Errorf("supervisor request failed"))
}

var _ persistentapp.Attachment = (*Client)(nil)
var _ persistentapp.ControlAttachment = (*Client)(nil)
var _ persistentapp.RecoveryAttachment = (*Client)(nil)
