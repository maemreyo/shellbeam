//go:build linux || darwin

package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type Client struct {
	mu          sync.Mutex
	conn        net.Conn
	reader      *bufio.Reader
	sessionID   string
	generation  string
	status      persistentapp.Status
	inputOffset int64
	killSeq     uint64
}

func DialAttachment(ctx context.Context, layout Layout, capability Capability, sessionID, generationID string) (*Client, persistentapp.Status, error) {
	if err := validateLayout(layout); err != nil {
		return nil, persistentapp.Status{}, err
	}
	metadata, err := LoadMetadata(layout)
	if err != nil || metadata.SessionID != sessionID || metadata.GenerationID != generationID {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": sessionID, "reason": "identity"}, nil)
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", layout.SocketPath)
	if err != nil {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": sessionID, "reason": "connect"}, nil)
	}
	client := &Client{conn: conn, reader: bufio.NewReaderSize(conn, MaxFrameBytes+1), sessionID: sessionID, generation: generationID}
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
	if len(data) == 0 {
		return fmt.Errorf("empty input")
	}
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindWrite, SessionID: c.sessionID, GenerationID: c.generation, Write: &WriteRequest{InputOffset: c.inputOffset, Chars: string(data)}})
	if err != nil {
		return err
	}
	if response.Write == nil {
		return failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": "write_response"}, nil)
	}
	c.inputOffset = response.Write.NextOffset
	return nil
}

func (c *Client) CloseStdin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindWrite, SessionID: c.sessionID, GenerationID: c.generation, Write: &WriteRequest{InputOffset: c.inputOffset, EOF: true}})
	if err != nil {
		return err
	}
	if response.Write == nil {
		return failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": "write_response"}, nil)
	}
	c.inputOffset = response.Write.NextOffset
	return nil
}

func (c *Client) Signal(name string) receipt.SignalEvidence {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killSeq++
	killID := fmt.Sprintf("proxy-kill-%d", c.killSeq)
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindSignal, SessionID: c.sessionID, GenerationID: c.generation, Signal: &SignalRequest{KillID: killID, Signal: name}})
	if err != nil || response.Signal == nil {
		return receipt.SignalEvidence{Requested: name}
	}
	return receipt.SignalEvidence{Requested: name, Attempted: response.Signal.Attempted, Succeeded: response.Signal.Succeeded}
}

func (c *Client) Wait(ctx context.Context) receipt.ExitEvidence {
	for {
		c.mu.Lock()
		status := c.status
		after := uint64(0)
		response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindWait, SessionID: c.sessionID, GenerationID: c.generation, Wait: &WaitRequest{AfterChange: after, WaitMS: MaxWaitMS}})
		if err == nil && response.Status != nil {
			status = c.applyStatusLocked(response.Status)
		}
		c.mu.Unlock()
		if err != nil {
			select {
			case <-ctx.Done():
				return receipt.ExitEvidence{}
			default:
				return receipt.ExitEvidence{}
			}
		}
		if status.State.Terminal() {
			return status.Exit
		}
		select {
		case <-ctx.Done():
			return receipt.ExitEvidence{}
		default:
		}
	}
}

func (c *Client) ReadOutput(ctx context.Context, offset int64, maxBytes int) ([]byte, int64, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, offset, 0, err
	}
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindOutput, SessionID: c.sessionID, GenerationID: c.generation, Output: &OutputRequest{Offset: offset, MaxBytes: maxBytes}})
	if err != nil {
		return nil, offset, 0, err
	}
	if response.Output == nil {
		return nil, offset, 0, failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": "output_response"}, nil)
	}
	return append([]byte(nil), response.Output.Data...), response.Output.NextOffset, response.Output.Extent, nil
}

func (c *Client) Status(ctx context.Context) (persistentapp.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return persistentapp.Status{}, err
	}
	response, err := c.roundTripLocked(Request{ProtocolVersion: ProtocolVersion, Kind: KindStatus, SessionID: c.sessionID, GenerationID: c.generation})
	if err != nil {
		return persistentapp.Status{}, err
	}
	if response.Status == nil {
		return persistentapp.Status{}, failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": "status_response"}, nil)
	}
	return c.applyStatusLocked(response.Status), nil
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
			return Response{}, failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": "error_response"}, nil)
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
		PID: status.PID, Spawn: status.Spawn, Exit: status.Exit,
	}
	c.status = result
	c.inputOffset = status.NextInputOffset
	return result
}

func responseFailure(value *ProtocolError) error {
	if value == nil {
		return failure.New(failure.Internal, nil, nil)
	}
	code := failure.Code(value.Code)
	return failure.New(code, value.Details, fmt.Errorf("supervisor request failed"))
}

var _ persistentapp.Attachment = (*Client)(nil)
var _ session.State
var _ = time.Second
