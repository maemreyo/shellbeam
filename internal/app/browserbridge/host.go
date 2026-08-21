package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// Decode applies deliberately asymmetric strictness.
//
// hello must stay parseable across protocol versions: a host that only
// implements a later version still has to answer an older hello, or a version
// mismatch becomes indistinguishable from a broken host and the extension
// cannot tell the user which remediation applies. Every other verb decodes
// strictly, so an unknown field is a rejected request rather than a silently
// ignored one.
func Decode(raw []byte) (protocol.Request, protocol.Response, bool) {
	var probe struct {
		ProtocolVersion int           `json:"protocol_version"`
		Verb            protocol.Verb `json:"verb"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return protocol.Request{}, invalid("malformed_request"), false
	}
	if probe.ProtocolVersion != protocol.ProtocolVersion {
		resp := base(probe.Verb, protocol.StatusProtocolIncompatible)
		resp.Reason = "protocol_incompatible"
		return protocol.Request{}, resp, false
	}
	if probe.Verb == protocol.VerbHello {
		return protocol.Request{ProtocolVersion: probe.ProtocolVersion, Verb: protocol.VerbHello}, protocol.Response{}, true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var req protocol.Request
	if err := decoder.Decode(&req); err != nil {
		return protocol.Request{}, invalid("unexpected_field"), false
	}
	if err := req.Validate(); err != nil {
		return protocol.Request{}, invalid(err.Error()), false
	}
	return req, protocol.Response{}, true
}

func invalid(reason string) protocol.Response {
	resp := base("", protocol.StatusInvalidRequest)
	resp.Reason = reason
	return resp
}

// Serve reads one message, answers it, and returns. The host process exits
// after this call; it holds no cursors, no watch state and no session.
func Serve(ctx context.Context, planner *Planner, in io.Reader, out io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(in, int64(protocol.MaxResponseBytes)+1))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return writeResponse(out, invalid("malformed_request"))
	}
	if len(raw) > protocol.MaxResponseBytes {
		return writeResponse(out, invalid("request_too_large"))
	}
	req, failure, ok := Decode(raw)
	resp := failure
	if ok {
		resp = dispatch(ctx, planner, req)
	}
	return writeResponse(out, resp)
}

func writeResponse(out io.Writer, resp protocol.Response) error {
	encoded, err := protocol.BoundResponse(resp)
	if err != nil {
		return err
	}
	_, err = out.Write(encoded)
	return err
}

func dispatch(ctx context.Context, planner *Planner, req protocol.Request) protocol.Response {
	switch req.Verb {
	case protocol.VerbHello:
		return base(protocol.VerbHello, protocol.StatusOK)
	case protocol.VerbActivityFacts:
		return planner.ActivityFacts(ctx, req.CorrelationID)
	case protocol.VerbActivityEvents:
		return planner.ActivityEvents(ctx, req.CorrelationID, req.Cursor)
	case protocol.VerbVerificationFacts:
		return planner.VerificationFacts(ctx, req.CorrelationID)
	case protocol.VerbStructuredFailureFacts:
		return planner.StructuredFailureFacts(ctx, req.CorrelationID)
	default:
		return invalid("unknown_verb")
	}
}
