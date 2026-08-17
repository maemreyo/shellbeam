//go:build darwin || linux

package localfs

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/media"
)

func FuzzDecodeConfig(f *testing.F) {
	f.Add([]byte("not-an-image"))
	f.Add([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = decodeImageConfig(data, media.V1Limits()) })
}
