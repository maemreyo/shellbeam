package browserbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

func TestFramingRoundTripsInNativeByteOrder(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFramed(&buf, []byte(`{"verb":"hello"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if buf.Len() != 4+len(`{"verb":"hello"}`) {
		t.Fatalf("frame length = %d", buf.Len())
	}
	got, err := ReadFramed(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"verb":"hello"}` {
		t.Fatalf("payload = %q", got)
	}
}

func TestReadFramedRejectsAnOversizedOrZeroLengthHeader(t *testing.T) {
	for _, length := range []uint32{0, protocol.MaxResponseBytes + 1} {
		var header [4]byte
		binary.NativeEndian.PutUint32(header[:], length)
		if _, err := ReadFramed(bytes.NewReader(header[:])); err == nil {
			t.Fatalf("length %d accepted", length)
		}
	}
}
