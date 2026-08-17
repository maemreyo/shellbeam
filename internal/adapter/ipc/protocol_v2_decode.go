package ipc

import (
	"fmt"
	"io"

	"github.com/maemreyo/shellbeam/internal/core/jsonstrict"
)

func readBoundedJSON(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, (1<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("request too large")
	}
	return data, nil
}

func strictDecodeV2(data []byte, out any) error {
	return jsonstrict.Decode(data, out)
}
