package mcp

import (
	"encoding/json"

	"github.com/maemreyo/shellbeam/internal/core/jsonstrict"
)

// decodeInput preserves the protocol-v1 decoding contract.
func decodeInput(raw []byte) (input, error) {
	var in input
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return input{}, err
	}
	return in, nil
}

func decodeInputV2(raw []byte) (input, error) {
	var in input
	if err := jsonstrict.Decode(raw, &in); err != nil {
		return input{}, err
	}
	return in, nil
}
