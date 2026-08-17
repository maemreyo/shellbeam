//go:build darwin || linux

package localfs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	"golang.org/x/sys/unix"
)

func logical(t *testing.T, raw string) media.LogicalPath {
	t.Helper()
	p, err := media.ParseLogicalPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fixtureImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x + 1), G: uint8(y + 2), B: 0x77, A: 0xff})
		}
	}
	var b bytes.Buffer
	switch format {
	case "png":
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
	case "jpeg":
		if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown fixture format %q", format)
	}
	return b.Bytes()
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestReaderReadsRegularPNGAndJPEGByContent(t *testing.T) {
	for _, tc := range []struct{ name, format, mime string }{{"png", "png", "image/png"}, {"jpeg", "jpeg", "image/jpeg"}} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			data := fixtureImage(t, tc.format, 3, 2)
			writeFile(t, filepath.Join(base, "image.bin"), data, 0o600)
			got, err := (Reader{}).Read(context.Background(), base, logical(t, "image.bin"), media.V1Limits())
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tc.format || got.MIMEType != tc.mime || got.Width != 3 || got.Height != 2 || !bytes.Equal(got.Data, data) {
				t.Fatalf("file=%#v bytes=%d", got, len(got.Data))
			}
		})
	}
}

func TestReaderRejectsUnsafeAndNonRegularPaths(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "rmfs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	writeFile(t, filepath.Join(base, "ok.png"), fixtureImage(t, "png", 2, 2), 0o600)
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ok.png", filepath.Join(base, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir", filepath.Join(base, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(base, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(base, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cases := []struct {
		path string
		code failure.Code
	}{
		{"link.png", failure.MediaPathUnsafe},
		{"linkdir/x.png", failure.MediaPathUnsafe},
		{"dir", failure.MediaNotRegular},
		{"fifo", failure.MediaNotRegular},
		{"sock", failure.MediaNotRegular},
		{"missing.png", failure.MediaPathNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			_, err := (Reader{}).Read(context.Background(), base, logical(t, tc.path), media.V1Limits())
			if !errors.Is(err, tc.code) {
				t.Fatalf("err=%v want=%s", err, tc.code)
			}
		})
	}
	if _, err := (Reader{}).Read(context.Background(), "/dev", logical(t, "null"), media.V1Limits()); !errors.Is(err, failure.MediaNotRegular) {
		t.Fatalf("device err=%v", err)
	}
}

func TestReaderPermissionDeniedIsSafeReadFailure(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "private.png")
	writeFile(t, p, fixtureImage(t, "png", 1, 1), 0o000)
	defer os.Chmod(p, 0o600)
	_, err := (Reader{}).Read(context.Background(), base, logical(t, "private.png"), media.V1Limits())
	if err == nil {
		t.Skip("host permits reading mode-000 owner file")
	}
	if !errors.Is(err, failure.MediaReadFailed) {
		t.Fatalf("err=%v", err)
	}
	pub := failure.Public(err)
	if len(pub.Details) != 0 {
		t.Fatalf("unsafe details=%#v", pub.Details)
	}
}

func TestReaderExactByteLimitAndLimitPlusOne(t *testing.T) {
	base := t.TempDir()
	pngData := fixtureImage(t, "png", 1, 1)
	exact := append(append([]byte(nil), pngData...), bytes.Repeat([]byte{0xA5}, media.MaxImageBytes-len(pngData))...)
	writeFile(t, filepath.Join(base, "exact.bin"), exact, 0o600)
	got, err := (Reader{}).Read(context.Background(), base, logical(t, "exact.bin"), media.V1Limits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != media.MaxImageBytes {
		t.Fatalf("bytes=%d", len(got.Data))
	}
	writeFile(t, filepath.Join(base, "too-big.bin"), append(exact, 0), 0o600)
	if _, err := (Reader{}).Read(context.Background(), base, logical(t, "too-big.bin"), media.V1Limits()); !errors.Is(err, failure.MediaTooLarge) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderDetectsSourceReplacementAndMutation(t *testing.T) {
	for _, mode := range []string{"replace", "write"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			name := "image.png"
			full := filepath.Join(base, name)
			writeFile(t, full, fixtureImage(t, "png", 2, 2), 0o600)
			var once sync.Once
			r := Reader{hooks: &readerHooks{checkpoint: func(stage string) {
				if stage != "before-post-fstat" {
					return
				}
				once.Do(func() {
					switch mode {
					case "replace":
						if err := os.Rename(full, full+".old"); err != nil {
							t.Error(err)
							return
						}
						writeFile(t, full, fixtureImage(t, "png", 3, 3), 0o600)
					case "write":
						f, err := os.OpenFile(full, os.O_WRONLY|os.O_APPEND, 0)
						if err != nil {
							t.Error(err)
							return
						}
						_, _ = f.Write([]byte{1})
						_ = f.Close()
					}
				})
			}}}
			_, err := (r).Read(context.Background(), base, logical(t, name), media.V1Limits())
			if !errors.Is(err, failure.MediaSourceChanged) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestReaderObservesCancellationAtControllableStages(t *testing.T) {
	stages := []string{"before-base-open", "after-base-open", "before-openat", "after-openat", "before-pre-fstat", "after-pre-fstat", "before-read", "after-read", "before-post-fstat", "after-post-fstat", "before-path-stat", "after-path-stat", "before-decode", "after-decode"}
	for _, target := range stages {
		t.Run(target, func(t *testing.T) {
			base := t.TempDir()
			if err := os.Mkdir(filepath.Join(base, "d"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(base, "d", "image.png"), fixtureImage(t, "png", 2, 2), 0o600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := false
			r := Reader{hooks: &readerHooks{checkpoint: func(stage string) {
				if stage == target && !seen {
					seen = true
					cancel()
				}
			}}}
			_, err := r.Read(ctx, base, logical(t, "d/image.png"), media.V1Limits())
			if !seen {
				t.Fatalf("stage %s not reached", target)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("stage %s err=%v", target, err)
			}
		})
	}
}

func TestBlockedReadDoesNotPretendCancellationInterruptsSyscall(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "image.png"), fixtureImage(t, "png", 2, 2), 0o600)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	r := Reader{hooks: &readerHooks{read: func(fd int, p []byte) (int, error) { close(entered); <-release; return unix.Read(fd, p) }}}
	go func() { _, err := r.Read(ctx, base, logical(t, "image.png"), media.V1Limits()); done <- err }()
	<-entered
	cancel()
	select {
	case err := <-done:
		t.Fatalf("returned before blocked read released: %v", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderReadsWebPByContent(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("UklGRjIAAABXRUJQVlA4TCUAAAAvAUAAAB8gECBECPPfIEggkOIghnhA5z96QQwoCRAARRmJ6H8MAA==")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "image.bin"), data, 0o600)
	got, err := (Reader{}).Read(context.Background(), base, logical(t, "image.bin"), media.V1Limits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != "webp" || got.MIMEType != "image/webp" || got.Width != 2 || got.Height != 2 {
		t.Fatalf("file=%#v", got)
	}
}

func TestReaderRejectsUnsupportedCorruptAndOversizedDimensions(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "text.bin"), []byte("hello"), 0o600)
	if _, err := (Reader{}).Read(context.Background(), base, logical(t, "text.bin"), media.V1Limits()); !errors.Is(err, failure.MediaTypeUnsupported) {
		t.Fatalf("unsupported err=%v", err)
	}
	writeFile(t, filepath.Join(base, "corrupt.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}, 0o600)
	if _, err := (Reader{}).Read(context.Background(), base, logical(t, "corrupt.png"), media.V1Limits()); !errors.Is(err, failure.MediaInvalidImage) {
		t.Fatalf("corrupt err=%v", err)
	}
	writeFile(t, filepath.Join(base, "wide.png"), fixtureImage(t, "png", 3, 2), 0o600)
	limits := media.V1Limits()
	limits.MaxWidth = 2
	if _, err := (Reader{}).Read(context.Background(), base, logical(t, "wide.png"), limits); !errors.Is(err, failure.MediaDimensionsExceeded) {
		t.Fatalf("width err=%v", err)
	}
	limits = media.V1Limits()
	limits.MaxPixels = 5
	if _, err := (Reader{}).Read(context.Background(), base, logical(t, "wide.png"), limits); !errors.Is(err, failure.MediaDimensionsExceeded) {
		t.Fatalf("pixels err=%v", err)
	}
}
