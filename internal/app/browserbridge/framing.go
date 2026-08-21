package browserbridge

import (
	"encoding/binary"
	"fmt"
	"io"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// ReadFramed reads one native-messaging frame: an unsigned 32-bit length in
// native byte order followed by that many UTF-8 JSON bytes.
func ReadFramed(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.NativeEndian.Uint32(header[:])
	if length == 0 || length > protocol.MaxResponseBytes {
		return nil, fmt.Errorf("framed message length %d out of range", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFramed writes one native-messaging frame. The protocol hard cap keeps
// this writer within the application budget even if a future caller bypasses
// BoundResponse.
func WriteFramed(w io.Writer, payload []byte) error {
	if len(payload) > protocol.MaxResponseBytes {
		return fmt.Errorf("framed response %d exceeds bound", len(payload))
	}
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
