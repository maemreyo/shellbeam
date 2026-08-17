package jsonstrict

import json "github.com/go-json-experiment/json"

// Decode unmarshals exactly one JSON value using strict modern semantics.
// Unknown members, duplicate names, invalid UTF-8, and case-mismatched names
// are rejected. Callers must not enable permissive JSON options at security
// boundaries.
func Decode(data []byte, dst any) error {
	return json.Unmarshal(data, dst, json.RejectUnknownMembers(true))
}
