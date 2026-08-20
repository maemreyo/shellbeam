//go:build darwin || linux

package localfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"golang.org/x/sys/unix"
)

type artifactSourceHandle struct {
	mu                 sync.Mutex
	parentFD           int
	fileFD             int
	captureAuthorityID string
	closed             bool
}

func (a *ArtifactPathAuthority) OpenArtifactSource(ctx context.Context, captureAuthorityID string, maxBlobBytes int64) (structuredapp.ArtifactSourceHandle, structuredapp.ArtifactSourceIdentity, error) {
	if a == nil || !validCaptureAuthorityID(captureAuthorityID) || maxBlobBytes < 1 || maxBlobBytes > structuredapp.MaxArtifactBlobBytes {
		return nil, structuredapp.ArtifactSourceIdentity{}, structuredapp.ErrArtifactCaptureUnavailable
	}
	a.mu.Lock()
	if a.closed || a.parentFD < 0 {
		a.mu.Unlock()
		return nil, structuredapp.ArtifactSourceIdentity{}, ErrArtifactAuthorityClosed
	}
	parentFD, finalName, hooks := a.parentFD, a.finalName, a.hooks
	a.parentFD = -1
	a.closed = true
	a.mu.Unlock()

	parentOwned := true
	defer func() {
		if parentOwned {
			_ = unix.Close(parentFD)
		}
	}()
	if err := artifactAuthorityCheckpoint(ctx, hooks, "before-final-open"); err != nil {
		return nil, structuredapp.ArtifactSourceIdentity{}, err
	}
	fileFD, err := unix.Openat(parentFD, finalName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, structuredapp.ArtifactSourceIdentity{}, artifactSourceOpenError(err)
	}
	fileOwned := true
	defer func() {
		if fileOwned {
			_ = unix.Close(fileFD)
		}
	}()
	if err := artifactAuthorityCheckpoint(ctx, hooks, "after-final-open"); err != nil {
		return nil, structuredapp.ArtifactSourceIdentity{}, err
	}
	identity, err := statArtifactSource(fileFD)
	if err != nil {
		return nil, structuredapp.ArtifactSourceIdentity{}, err
	}
	if identity.Size > maxBlobBytes {
		return nil, structuredapp.ArtifactSourceIdentity{}, structuredapp.ErrArtifactSourceBudgetExceeded
	}
	handle := &artifactSourceHandle{parentFD: parentFD, fileFD: fileFD, captureAuthorityID: captureAuthorityID}
	parentOwned, fileOwned = false, false
	return handle, identity, nil
}

func artifactAuthorityCheckpoint(ctx context.Context, hooks *artifactCaptureHooks, stage string) error {
	if hooks != nil && hooks.checkpoint != nil {
		hooks.checkpoint(stage)
	}
	return ctx.Err()
}

func artifactSourceOpenError(err error) error {
	switch {
	case errors.Is(err, unix.ENOENT):
		return structuredapp.ErrArtifactSourceMissing
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
		return structuredapp.ErrArtifactSourceKindMismatch
	default:
		return fmt.Errorf("%w: %v", structuredapp.ErrArtifactCaptureUnavailable, err)
	}
}

func statArtifactSource(fd int) (structuredapp.ArtifactSourceIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return structuredapp.ArtifactSourceIdentity{}, fmt.Errorf("%w: %v", structuredapp.ErrArtifactCaptureUnavailable, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return structuredapp.ArtifactSourceIdentity{}, structuredapp.ErrArtifactSourceKindMismatch
	}
	mtimeNS, ctimeNS := artifactStatTimes(st)
	canonical, err := json.Marshal(struct {
		Version int    `json:"version"`
		Scheme  string `json:"scheme"`
		Dev     uint64 `json:"dev"`
		Ino     uint64 `json:"ino"`
		Mode    uint32 `json:"mode"`
		Size    int64  `json:"size"`
		MtimeNS int64  `json:"mtime_ns"`
		CtimeNS int64  `json:"ctime_ns"`
	}{
		Version: 1, Scheme: structuredapp.ArtifactSourceIdentityUnixV1,
		Dev: uint64(st.Dev), Ino: uint64(st.Ino), Mode: uint32(st.Mode), Size: st.Size,
		MtimeNS: mtimeNS, CtimeNS: ctimeNS,
	})
	if err != nil {
		return structuredapp.ArtifactSourceIdentity{}, err
	}
	sum := sha256.Sum256(canonical)
	return structuredapp.ArtifactSourceIdentity{Scheme: structuredapp.ArtifactSourceIdentityUnixV1, Digest: hex.EncodeToString(sum[:]), Size: st.Size}, nil
}

func validCaptureAuthorityID(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (h *artifactSourceHandle) Read(p []byte) (int, error) {
	if h == nil {
		return 0, ErrArtifactAuthorityClosed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.fileFD < 0 {
		return 0, ErrArtifactAuthorityClosed
	}
	for {
		n, err := unix.Read(h.fileFD, p)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if n == 0 && err == nil {
			return 0, io.EOF
		}
		return n, err
	}
}

func (h *artifactSourceHandle) StatIdentity() (structuredapp.ArtifactSourceIdentity, error) {
	if h == nil {
		return structuredapp.ArtifactSourceIdentity{}, ErrArtifactAuthorityClosed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.fileFD < 0 {
		return structuredapp.ArtifactSourceIdentity{}, ErrArtifactAuthorityClosed
	}
	return statArtifactSource(h.fileFD)
}

func (h *artifactSourceHandle) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	fileFD, parentFD := h.fileFD, h.parentFD
	h.fileFD, h.parentFD = -1, -1
	var err error
	if fileFD >= 0 {
		err = errors.Join(err, unix.Close(fileFD))
	}
	if parentFD >= 0 {
		err = errors.Join(err, unix.Close(parentFD))
	}
	return err
}

func (a *ArtifactPathAuthority) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	fd := a.parentFD
	a.parentFD = -1
	if fd < 0 {
		return nil
	}
	return unix.Close(fd)
}
