//go:build darwin || linux

package localfs

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	_ "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
)

const readChunkBytes = 64 << 10

type readerHooks struct {
	checkpoint func(stage string)
	read       func(fd int, p []byte) (int, error)
}

type Reader struct {
	hooks *readerHooks
}

type fileIdentity struct {
	dev, ino          uint64
	mode              uint32
	size              int64
	mtimeSec, mtimeNS int64
	ctimeSec, ctimeNS int64
}

type imageMeta struct {
	mimeType string
	format   string
	width    int
	height   int
}

func (r Reader) checkpoint(ctx context.Context, stage string) error {
	if r.hooks != nil && r.hooks.checkpoint != nil {
		r.hooks.checkpoint(stage)
	}
	return ctx.Err()
}

func (r Reader) Read(ctx context.Context, base string, path media.LogicalPath, limits media.Limits) (media.File, error) {
	if err := validateReadInput(base, path, limits); err != nil {
		return media.File{}, err
	}
	parentFD, fileFD, finalName, err := r.openPinned(ctx, base, path)
	if err != nil {
		return media.File{}, err
	}
	defer unix.Close(parentFD)
	defer unix.Close(fileFD)

	pre, err := r.statFinal(ctx, fileFD, "pre")
	if err != nil {
		return media.File{}, err
	}
	if pre.Mode&unix.S_IFMT != unix.S_IFREG {
		return media.File{}, failure.New(failure.MediaNotRegular, nil, nil)
	}
	if pre.Size > int64(limits.MaxImageBytes) {
		return media.File{}, failure.New(failure.MediaTooLarge, nil, nil)
	}
	before := identityOf(pre)
	data, err := r.readBounded(ctx, fileFD, limits.MaxImageBytes)
	if err != nil {
		return media.File{}, err
	}
	post, err := r.statFinal(ctx, fileFD, "post")
	if err != nil {
		return media.File{}, err
	}
	if identityOf(post) != before {
		return media.File{}, failure.New(failure.MediaSourceChanged, nil, nil)
	}
	if err := r.verifyPathIdentity(ctx, parentFD, finalName, before); err != nil {
		return media.File{}, err
	}
	if err := r.checkpoint(ctx, "before-decode"); err != nil {
		return media.File{}, err
	}
	meta, err := decodeImageConfig(data, limits)
	if err != nil {
		return media.File{}, err
	}
	if err := r.checkpoint(ctx, "after-decode"); err != nil {
		return media.File{}, err
	}
	return media.File{MIMEType: meta.mimeType, Format: meta.format, Width: meta.width, Height: meta.height, Data: data}, nil
}

func validateReadInput(base string, path media.LogicalPath, limits media.Limits) error {
	if base == "" || base[0] != '/' || path.Raw == "" || len(path.Components) == 0 {
		return failure.New(failure.MediaPathUnsafe, nil, nil)
	}
	parsed, err := media.ParseLogicalPath(path.Raw)
	if err != nil || len(parsed.Components) != len(path.Components) {
		return failure.New(failure.MediaPathUnsafe, nil, err)
	}
	for i := range parsed.Components {
		if parsed.Components[i] != path.Components[i] {
			return failure.New(failure.MediaPathUnsafe, nil, nil)
		}
	}
	if limits.MaxImageBytes <= 0 || limits.MaxWidth <= 0 || limits.MaxHeight <= 0 || limits.MaxPixels <= 0 {
		return failure.New(failure.MediaReadFailed, nil, nil)
	}
	return nil
}

func (r Reader) openPinned(ctx context.Context, base string, path media.LogicalPath) (int, int, string, error) {
	if err := r.checkpoint(ctx, "before-base-open"); err != nil {
		return -1, -1, "", err
	}
	parent, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, -1, "", mapOpenFailure(err)
	}
	if err := r.checkpoint(ctx, "after-base-open"); err != nil {
		unix.Close(parent)
		return -1, -1, "", err
	}
	for _, component := range path.Components[:len(path.Components)-1] {
		next, err := r.openAt(ctx, parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
		if err != nil {
			unix.Close(parent)
			return -1, -1, "", err
		}
		unix.Close(parent)
		parent = next
	}
	name := path.Components[len(path.Components)-1]
	fd, err := r.openAt(ctx, parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK)
	if err != nil {
		unix.Close(parent)
		return -1, -1, "", err
	}
	return parent, fd, name, nil
}

func (r Reader) openAt(ctx context.Context, parent int, name string, flags int) (int, error) {
	if err := r.checkpoint(ctx, "before-openat"); err != nil {
		return -1, err
	}
	fd, err := unix.Openat(parent, name, flags, 0)
	if err != nil {
		return -1, mapOpenFailure(err)
	}
	if err := r.checkpoint(ctx, "after-openat"); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func (r Reader) statFinal(ctx context.Context, fd int, phase string) (unix.Stat_t, error) {
	beforeStage, afterStage := "before-pre-fstat", "after-pre-fstat"
	if phase == "post" {
		beforeStage, afterStage = "before-post-fstat", "after-post-fstat"
	}
	if err := r.checkpoint(ctx, beforeStage); err != nil {
		return unix.Stat_t{}, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return unix.Stat_t{}, mapReadFailure(err)
	}
	if err := r.checkpoint(ctx, afterStage); err != nil {
		return unix.Stat_t{}, err
	}
	return st, nil
}

func (r Reader) verifyPathIdentity(ctx context.Context, parent int, name string, want fileIdentity) error {
	if err := r.checkpoint(ctx, "before-path-stat"); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return failure.New(failure.MediaSourceChanged, nil, errnoOnly(err))
	}
	if err := r.checkpoint(ctx, "after-path-stat"); err != nil {
		return err
	}
	if identityOf(st) != want {
		return failure.New(failure.MediaSourceChanged, nil, nil)
	}
	return nil
}

func (r Reader) readBounded(ctx context.Context, fd int, max int) ([]byte, error) {
	data := make([]byte, 0, min(max, readChunkBytes))
	buf := make([]byte, readChunkBytes)
	for {
		if err := r.checkpoint(ctx, "before-read"); err != nil {
			return nil, err
		}
		n, err := r.read(fd, buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			if len(data) > max {
				return nil, failure.New(failure.MediaTooLarge, nil, nil)
			}
		}
		if err := r.checkpoint(ctx, "after-read"); err != nil {
			return nil, err
		}
		if err == nil && n == 0 {
			break
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, mapReadFailure(err)
		}
		if n < len(buf) {
			// Direct unix.Read reports EOF as n=0, err=nil. Confirm it with
			// one more bounded read rather than assuming short read == EOF.
			continue
		}
	}
	return data, nil
}

func (r Reader) read(fd int, p []byte) (int, error) {
	if r.hooks != nil && r.hooks.read != nil {
		return r.hooks.read(fd, p)
	}
	return unix.Read(fd, p)
}

func identityOf(st unix.Stat_t) fileIdentity {
	return fileIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino), mode: uint32(st.Mode), size: st.Size, mtimeSec: st.Mtim.Sec, mtimeNS: st.Mtim.Nsec, ctimeSec: st.Ctim.Sec, ctimeNS: st.Ctim.Nsec}
}

func decodeImageConfig(data []byte, limits media.Limits) (imageMeta, error) {
	expected := sniffFormat(data)
	if expected == "" {
		return imageMeta{}, failure.New(failure.MediaTypeUnsupported, nil, nil)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != expected {
		return imageMeta{}, failure.New(failure.MediaInvalidImage, nil, nil)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > limits.MaxWidth || cfg.Height > limits.MaxHeight || int64(cfg.Width)*int64(cfg.Height) > limits.MaxPixels {
		return imageMeta{}, failure.New(failure.MediaDimensionsExceeded, nil, nil)
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	return imageMeta{mimeType: mime, format: format, width: cfg.Width, height: cfg.Height}, nil
}

func sniffFormat(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "jpeg"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "webp"
	}
	return ""
}

func mapOpenFailure(err error) error {
	err = errnoOnly(err)
	switch {
	case errors.Is(err, unix.ENOENT):
		return failure.New(failure.MediaPathNotFound, nil, err)
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
		return failure.New(failure.MediaPathUnsafe, nil, err)
	case errors.Is(err, unix.EOPNOTSUPP), errors.Is(err, unix.ENXIO):
		return failure.New(failure.MediaNotRegular, nil, err)
	default:
		return failure.New(failure.MediaReadFailed, nil, err)
	}
}

func mapReadFailure(err error) error {
	if err == nil {
		return nil
	}
	return failure.New(failure.MediaReadFailed, nil, errnoOnly(err))
}

func errnoOnly(err error) error {
	var errno unix.Errno
	if errors.As(err, &errno) {
		return errno
	}
	return errors.New("local media syscall failed")
}
