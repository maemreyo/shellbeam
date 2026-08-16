package supervisor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type ProtocolError struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Details   map[string]string `json:"details,omitempty"`
}

type StatusResponse struct {
	State               session.State          `json:"state"`
	Outcome             session.Outcome        `json:"outcome,omitempty"`
	Change              uint64                 `json:"change"`
	PID                 int                    `json:"pid,omitempty"`
	OutputBytes         int64                  `json:"output_bytes"`
	OutputAcknowledged  int64                  `json:"output_acknowledged"`
	InputAcceptedBytes  int64                  `json:"input_accepted_bytes"`
	InputDeliveredBytes int64                  `json:"input_delivered_bytes"`
	NextInputOffset     int64                  `json:"next_input_offset"`
	StdinClosed         bool                   `json:"stdin_closed"`
	Spawn               receipt.SpawnEvidence  `json:"spawn_evidence"`
	Exit                receipt.ExitEvidence   `json:"exit_evidence"`
	Signal              receipt.SignalEvidence `json:"signal_evidence"`
	FailureReason       string                 `json:"failure_reason,omitempty"`
}

type OutputResponse struct {
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Extent     int64  `json:"extent"`
	Data       []byte `json:"data"`
}

type OutputAckResponse struct {
	Acknowledged int64 `json:"acknowledged"`
}

type WriteResponse struct {
	AcceptedBytes int   `json:"accepted_bytes"`
	NextOffset    int64 `json:"next_offset"`
	Duplicate     bool  `json:"duplicate"`
	EOFDelivered  bool  `json:"eof_delivered"`
}

type Response struct {
	ProtocolVersion int                `json:"protocol_version"`
	Kind            Kind               `json:"kind"`
	SessionID       string             `json:"session_id"`
	GenerationID    string             `json:"generation_id"`
	OK              bool               `json:"ok"`
	Authenticated   bool               `json:"authenticated,omitempty"`
	Status          *StatusResponse    `json:"status,omitempty"`
	Output          *OutputResponse    `json:"output,omitempty"`
	OutputAck       *OutputAckResponse `json:"output_ack,omitempty"`
	Write           *WriteResponse     `json:"write,omitempty"`
	Signal          *KillRecord        `json:"signal,omitempty"`
	Error           *ProtocolError     `json:"error,omitempty"`
}

func EncodeRequest(writer io.Writer, request Request) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return writeProtocolFrame(writer, request)
}

func DecodeResponse(reader *bufio.Reader) (Response, error) {
	raw, err := readProtocolFrame(reader)
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := decodeStrictFrame(raw, &response); err != nil {
		return Response{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || response.SessionID == "" || response.GenerationID == "" || response.OK == (response.Error != nil) {
		return Response{}, fmt.Errorf("invalid supervisor response")
	}
	return response, nil
}

func readRequestFrame(reader *bufio.Reader) (Request, error) {
	raw, err := readProtocolFrame(reader)
	if err != nil {
		return Request{}, err
	}
	var request Request
	if err := decodeStrictFrame(raw, &request); err != nil {
		return Request{}, protocolFailure("decode")
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func writeResponse(writer io.Writer, response Response) error {
	return writeProtocolFrame(writer, response)
}

func writeProtocolFrame(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded)+1 > MaxFrameBytes {
		return fmt.Errorf("supervisor frame exceeds limit")
	}
	encoded = append(encoded, '\n')
	written := 0
	for written < len(encoded) {
		n, err := writer.Write(encoded[written:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		written += n
	}
	return nil
}

func readProtocolFrame(reader *bufio.Reader) ([]byte, error) {
	var buffer bytes.Buffer
	for buffer.Len() <= MaxFrameBytes {
		part, err := reader.ReadSlice('\n')
		buffer.Write(part)
		if err == nil {
			raw := buffer.Bytes()
			if len(raw) > MaxFrameBytes || len(raw) < 2 {
				return nil, fmt.Errorf("invalid supervisor frame")
			}
			return append([]byte(nil), raw[:len(raw)-1]...), nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
	return nil, fmt.Errorf("supervisor frame exceeds limit")
}

func decodeStrictFrame(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing supervisor frame data")
	}
	return nil
}

func responseError(kind Kind, sessionID, generationID string, err error) Response {
	public := failure.Public(err)
	return Response{
		ProtocolVersion: ProtocolVersion, Kind: kind, SessionID: sessionID, GenerationID: generationID,
		Error: &ProtocolError{Code: string(public.Code), Message: public.Message, Retryable: public.Retryable, Details: public.Details},
	}
}

func statusResponse(status RuntimeStatus) *StatusResponse {
	return &StatusResponse{
		State: status.State, Outcome: status.Outcome, Change: status.Change, PID: status.PID, OutputBytes: status.OutputBytes, OutputAcknowledged: status.OutputAcknowledged,
		InputAcceptedBytes: status.Input.AcceptedBytes, InputDeliveredBytes: status.Input.DeliveredBytes, NextInputOffset: status.Input.NextOffset,
		StdinClosed: status.Input.EOFDelivered, Spawn: status.Spawn, Exit: status.Exit, Signal: status.Signal, FailureReason: status.FailureReason,
	}
}
