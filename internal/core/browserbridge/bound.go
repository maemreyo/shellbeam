package browserbridge

import "encoding/json"

// BoundResponse encodes a response within the protocol budget.
//
// Truncation is always recorded. A silently shortened response would let the
// extension read a partial fact set as a complete one, which the design
// forbids: coverage travels with the facts rather than being inferred.
func BoundResponse(resp Response) ([]byte, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	if len(raw) <= TargetResponseBytes {
		return raw, nil
	}
	for _, step := range []struct {
		reason string
		apply  func(*Response)
	}{
		{"failing_cases_dropped", func(r *Response) {
			for i := range r.Structured {
				r.Structured[i].FailingCases = nil
				r.Structured[i].CasesTruncated = true
			}
		}},
		{"event_kinds_dropped", func(r *Response) {
			if r.Events != nil {
				r.Events.Kinds = nil
			}
		}},
		{"structured_entries_dropped", func(r *Response) {
			if len(r.Structured) > 1 {
				r.Structured = r.Structured[:1]
			}
		}},
	} {
		step.apply(&resp)
		resp.Coverage.Truncated = true
		resp.Coverage.TruncationReason = step.reason
		if raw, err = json.Marshal(resp); err != nil {
			return nil, err
		}
		if len(raw) <= TargetResponseBytes {
			return raw, nil
		}
	}
	if len(raw) > MaxResponseBytes {
		minimal := Response{ProtocolVersion: resp.ProtocolVersion, SupportedVersions: resp.SupportedVersions, Verb: resp.Verb, Status: resp.Status, Coverage: Coverage{Truncated: true, TruncationReason: "response_exceeded_hard_cap"}}
		return json.Marshal(minimal)
	}
	return raw, nil
}
