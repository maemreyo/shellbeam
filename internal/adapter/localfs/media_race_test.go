//go:build darwin || linux

package localfs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/media"
)

func TestReaderConcurrentReadsAreIndependent(t *testing.T) {
	base := t.TempDir()
	data := fixtureImage(t, "png", 2, 2)
	if err := os.WriteFile(filepath.Join(base, "image.png"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	p := logical(t, "image.png")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := (Reader{}).Read(context.Background(), base, p, media.V1Limits())
			if err != nil {
				t.Errorf("read: %v", err)
				return
			}
			if len(got.Data) != len(data) {
				t.Errorf("bytes=%d", len(got.Data))
			}
		}()
	}
	wg.Wait()
}
