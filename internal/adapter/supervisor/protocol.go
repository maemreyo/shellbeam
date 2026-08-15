package supervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	ProtocolVersion = 1
	MaxFrameBytes   = 64 << 10
	MaxOutputBytes  = 32 << 10
	MaxWriteBytes   = 32 << 10
	MaxWaitMS       = 5000
)

type Kind string

const (
	KindHandshake Kind = "handshake"
	KindStatus    Kind = "status"
	KindOutput    Kind = "output"
	KindWrite     Kind = "write"
	KindSignal    Kind = "signal"
	KindWait      Kind = "wait"
)

type Request struct {
	ProtocolVersion int               `json:"protocol_version"`
	Kind            Kind              `json:"kind"`
	SessionID       string            `json:"session_id"`
	GenerationID    string            `json:"generation_id"`
	Handshake       *HandshakeRequest `json:"handshake,omitempty"`
	Output          *OutputRequest    `json:"output,omitempty"`
	Write           *WriteRequest     `json:"write,omitempty"`
	Signal          *SignalRequest    `json:"signal,omitempty"`
	Wait            *WaitRequest      `json:"wait,omitempty"`
}

type HandshakeRequest struct {
	Challenge string `json:"challenge"`
	Proof     string `json:"proof"`
}

type OutputRequest struct {
	Offset   int64 `json:"offset"`
	MaxBytes int   `json:"max_bytes"`
}

type WriteRequest struct {
	InputOffset int64  `json:"input_offset"`
	Chars       string `json:"chars,omitempty"`
	EOF         bool   `json:"eof,omitempty"`
}

type SignalRequest struct {
	KillID string `json:"kill_id"`
	Signal string `json:"signal"`
}

type WaitRequest struct {
	AfterChange uint64 `json:"after_change"`
	WaitMS      int    `json:"wait_ms"`
}

func DecodeRequest(reader io.Reader) (Request, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaxFrameBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > MaxFrameBytes {
		return Request{}, protocolFailure("frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, protocolFailure("decode")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Request{}, protocolFailure("trailing")
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func (r Request) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return protocolFailure("version")
	}
	if _, err := operation.ParseSessionID(r.SessionID); err != nil || !validOpaque(r.GenerationID) {
		return protocolFailure("identity")
	}
	payloads := 0
	for _, present := range []bool{r.Handshake != nil, r.Output != nil, r.Write != nil, r.Signal != nil, r.Wait != nil} {
		if present {
			payloads++
		}
	}
	switch r.Kind {
	case KindHandshake:
		if payloads != 1 || r.Handshake == nil || !safeToken(r.Handshake.Challenge, 128) || !safeToken(r.Handshake.Proof, 256) {
			return protocolFailure("handshake")
		}
	case KindStatus:
		if payloads != 0 {
			return protocolFailure("status")
		}
	case KindOutput:
		if payloads != 1 || r.Output == nil || r.Output.Offset < 0 || r.Output.MaxBytes < 1 || r.Output.MaxBytes > MaxOutputBytes {
			return protocolFailure("output")
		}
	case KindWrite:
		if payloads != 1 || r.Write == nil || r.Write.InputOffset < 0 || (r.Write.Chars == "") == !r.Write.EOF || len(r.Write.Chars) > MaxWriteBytes {
			return protocolFailure("write")
		}
	case KindSignal:
		if payloads != 1 || r.Signal == nil {
			return protocolFailure("signal")
		}
		if _, err := operation.ParseID(r.Signal.KillID); err != nil || (r.Signal.Signal != "INT" && r.Signal.Signal != "TERM" && r.Signal.Signal != "KILL") {
			return protocolFailure("signal")
		}
	case KindWait:
		if payloads != 1 || r.Wait == nil || r.Wait.WaitMS < 0 || r.Wait.WaitMS > MaxWaitMS {
			return protocolFailure("wait")
		}
	default:
		return protocolFailure("kind")
	}
	return nil
}

func safeToken(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func protocolFailure(reason string) error {
	return failure.New(failure.SupervisorProtocolMismatch, map[string]string{"reason": reason}, fmt.Errorf("invalid supervisor protocol frame"))
}
