package contextexec

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

const (
	ProtocolVersion = 1
	MaxFrameBytes   = 128 << 10
)

type MessageKind string

const (
	KindHello     MessageKind = "hello"
	KindChallenge MessageKind = "challenge"
	KindProof     MessageKind = "proof"
	KindRequest   MessageKind = "request"
	KindOutput    MessageKind = "output"
	KindTerminal  MessageKind = "terminal"
)

type HelloFrame struct {
	ProtocolVersion int         `json:"protocol_version"`
	Kind            MessageKind `json:"kind"`
	OpaqueLaunchID  string      `json:"opaque_launch_id"`
}

func (f HelloFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindHello || !validOpaque(f.OpaqueLaunchID, core.MaxOpaqueRefBytes) {
		return fmt.Errorf("invalid context helper hello")
	}
	return nil
}

type ChallengeFrame struct {
	ProtocolVersion int           `json:"protocol_version"`
	Kind            MessageKind   `json:"kind"`
	Identity        ClaimIdentity `json:"identity"`
	Challenge       string        `json:"challenge"`
}

func (f ChallengeFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindChallenge || f.Identity.Validate() != nil || !validChallenge(f.Challenge) {
		return fmt.Errorf("invalid context helper challenge")
	}
	return nil
}

type ProofFrame struct {
	ProtocolVersion int         `json:"protocol_version"`
	Kind            MessageKind `json:"kind"`
	Proof           string      `json:"proof"`
}

func (f ProofFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindProof || !validProof(f.Proof) {
		return fmt.Errorf("invalid context helper proof")
	}
	return nil
}

type RequestFrame struct {
	ProtocolVersion int                 `json:"protocol_version"`
	Kind            MessageKind         `json:"kind"`
	Request         core.Request        `json:"request"`
	Context         core.ContextBinding `json:"context"`
	Helper          core.HelperBinding  `json:"helper"`
}

func (f RequestFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindRequest {
		return fmt.Errorf("invalid context helper request frame")
	}
	if err := f.Request.Validate(); err != nil {
		return err
	}
	if err := f.Context.Validate(); err != nil {
		return err
	}
	if err := f.Helper.Validate(); err != nil {
		return err
	}
	fp, err := f.Request.Fingerprint()
	if err != nil {
		return err
	}
	if fp != f.Helper.RequestFingerprint || f.Context.SessionID != f.Request.SessionID || f.Context.AuthorityEpoch != f.Request.AuthorityEpoch {
		return fmt.Errorf("context helper request binding mismatch")
	}
	return nil
}

type OutputStream string

const (
	StreamStdout OutputStream = "stdout"
	StreamStderr OutputStream = "stderr"
)

type OutputFrame struct {
	ProtocolVersion int          `json:"protocol_version"`
	Kind            MessageKind  `json:"kind"`
	Stream          OutputStream `json:"stream"`
	Offset          int64        `json:"offset"`
	Data            []byte       `json:"data"`
}

func (f OutputFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindOutput || (f.Stream != StreamStdout && f.Stream != StreamStderr) || f.Offset < 0 || len(f.Data) > MaxFrameBytes/2 {
		return fmt.Errorf("invalid context helper output frame")
	}
	return nil
}

type TerminalFrame struct {
	ProtocolVersion int         `json:"protocol_version"`
	Kind            MessageKind `json:"kind"`
	Result          core.Result `json:"result"`
}

func (f TerminalFrame) Validate() error {
	if f.ProtocolVersion != ProtocolVersion || f.Kind != KindTerminal {
		return fmt.Errorf("invalid context helper terminal frame")
	}
	return f.Result.Validate()
}

func writeFrame(w io.Writer, value any) error {
	validator, ok := value.(interface{ Validate() error })
	if !ok {
		return fmt.Errorf("context helper frame lacks validation")
	}
	if err := validator.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > MaxFrameBytes {
		return fmt.Errorf("context helper frame too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

func readRawFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header[:]))
	if size <= 0 || size > MaxFrameBytes {
		return nil, fmt.Errorf("invalid context helper frame size")
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func decodeTypedFrame(raw []byte, out interface{ Validate() error }) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid context helper frame: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid context helper frame trailing data")
	}
	return out.Validate()
}
func readTypedFrame(r io.Reader, out interface{ Validate() error }) error {
	raw, err := readRawFrame(r)
	if err != nil {
		return err
	}
	return decodeTypedFrame(raw, out)
}
func frameKind(raw []byte) (MessageKind, error) {
	var base struct {
		ProtocolVersion int         `json:"protocol_version"`
		Kind            MessageKind `json:"kind"`
	}
	if err := json.Unmarshal(raw, &base); err != nil || base.ProtocolVersion != ProtocolVersion {
		return "", fmt.Errorf("invalid context helper frame envelope")
	}
	switch base.Kind {
	case KindOutput, KindTerminal:
		return base.Kind, nil
	default:
		return "", fmt.Errorf("unexpected context helper result frame")
	}
}
func readHelloFrame(r io.Reader) (HelloFrame, error) {
	var v HelloFrame
	return v, readTypedFrame(r, &v)
}
func readChallengeFrame(r io.Reader) (ChallengeFrame, error) {
	var v ChallengeFrame
	return v, readTypedFrame(r, &v)
}
func readProofFrame(r io.Reader) (ProofFrame, error) {
	var v ProofFrame
	return v, readTypedFrame(r, &v)
}
func readRequestFrame(r io.Reader) (RequestFrame, error) {
	var v RequestFrame
	return v, readTypedFrame(r, &v)
}
func readOutputFrame(r io.Reader) (OutputFrame, error) {
	var v OutputFrame
	return v, readTypedFrame(r, &v)
}
func readTerminalFrame(r io.Reader) (TerminalFrame, error) {
	var v TerminalFrame
	return v, readTypedFrame(r, &v)
}
