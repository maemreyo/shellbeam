package contextexec

import (
	"context"
	"fmt"
	"net"
)

type Client struct {
	Conn           net.Conn
	OpaqueLaunchID string
}

func (c *Client) Authenticate(ctx context.Context) (RequestFrame, error) {
	if c.Conn == nil {
		return RequestFrame{}, fmt.Errorf("context helper connection missing")
	}
	if err := ctx.Err(); err != nil {
		return RequestFrame{}, err
	}
	hello := HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: c.OpaqueLaunchID}
	if err := writeFrame(c.Conn, hello); err != nil {
		return RequestFrame{}, err
	}
	challenge, err := readChallengeFrame(c.Conn)
	if err != nil {
		return RequestFrame{}, err
	}
	if challenge.Identity.OpaqueLaunchID != c.OpaqueLaunchID {
		return RequestFrame{}, fmt.Errorf("context helper challenge launch mismatch")
	}
	capability, err := readCapability(c.Conn)
	if err != nil {
		return RequestFrame{}, err
	}
	proof, err := ClaimProof(capability, challenge.Identity, challenge.Challenge)
	if err != nil {
		return RequestFrame{}, err
	}
	if err := writeFrame(c.Conn, ProofFrame{ProtocolVersion: ProtocolVersion, Kind: KindProof, Proof: proof}); err != nil {
		return RequestFrame{}, err
	}
	request, err := readRequestFrame(c.Conn)
	if err != nil {
		return RequestFrame{}, err
	}
	fp, err := request.Request.Fingerprint()
	if err != nil {
		return RequestFrame{}, err
	}
	if request.Request.ContextExecID != challenge.Identity.ContextExecID || request.Request.SessionID != challenge.Identity.SessionID || request.Request.AuthorityEpoch != challenge.Identity.AuthorityEpoch || fp != challenge.Identity.RequestFingerprint || request.Helper.Generation != challenge.Identity.Generation || request.Helper.OpaqueLaunchID != challenge.Identity.OpaqueLaunchID {
		return RequestFrame{}, fmt.Errorf("context helper request identity mismatch")
	}
	return request, nil
}

func (c *Client) SendOutput(frame OutputFrame) error {
	if c.Conn == nil {
		return fmt.Errorf("context helper connection missing")
	}
	return writeFrame(c.Conn, frame)
}
func (c *Client) SendTerminal(frame TerminalFrame) error {
	if c.Conn == nil {
		return fmt.Errorf("context helper connection missing")
	}
	return writeFrame(c.Conn, frame)
}
