//go:build linux || darwin

package localfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"golang.org/x/sys/unix"
)

type sourceRoot struct {
	fd int
}

func openSourceRoot(root string) (*sourceRoot, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return &sourceRoot{fd: fd}, nil
}

func (r *sourceRoot) Close() error {
	if r == nil || r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}

func (r *sourceRoot) ReadRegular(rel string, maxBytes int64) ([]byte, bool, string, error) {
	if r == nil || r.fd < 0 || maxBytes <= 0 {
		return nil, false, "", fmt.Errorf("invalid hermetic source reader")
	}
	if selector, err := core.ParseRepoInputSelector(rel); err != nil || selector.Recursive || selector.Path != rel {
		return nil, false, "", fmt.Errorf("invalid hermetic source path")
	}
	parent, name, err := openParentNoFollow(r.fd, rel)
	if err != nil {
		return nil, false, "", err
	}
	defer unix.Close(parent)
	before, err := statAtNoFollow(parent, name)
	if err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, "", fmt.Errorf("hermetic source file changed type")
	}
	if before.Size < 0 || before.Size > maxBytes {
		return nil, false, "", fmt.Errorf("hermetic source file exceeds limit")
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, "", err
	}
	file := os.NewFile(uintptr(fd), rel)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, "", fmt.Errorf("open hermetic source file")
	}
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil || !sameStableStat(before, opened) {
		return nil, false, "", fmt.Errorf("hermetic source changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, false, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, false, "", fmt.Errorf("hermetic source file exceeds limit")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameStableStat(opened, after) || after.Size != int64(len(data)) {
		return nil, false, "", fmt.Errorf("hermetic source changed during read")
	}
	sum := sha256.Sum256(data)
	return data, after.Mode&0o111 != 0, hex.EncodeToString(sum[:]), nil
}

func openParentNoFollow(rootFD int, rel string) (int, string, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return -1, "", fmt.Errorf("invalid hermetic source path")
	}
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, "", err
	}
	for _, component := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, "", openErr
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}

func statAtNoFollow(parent int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

func verifySourceUnchanged(ctx context.Context, root string, selectors []string, limits core.CaptureLimits, before []selectedFile, manifest core.CaptureManifest) error {
	after, err := selectSourceFiles(ctx, root, selectors, limits)
	if err != nil {
		return err
	}
	if !sameSelected(before, after) || len(after) != len(manifest.Entries) {
		return fmt.Errorf("hermetic source selection changed during capture")
	}
	reader, err := openSourceRoot(root)
	if err != nil {
		return err
	}
	defer reader.Close()
	for i, source := range after {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, executable, digest, err := reader.ReadRegular(source.Path, limits.MaxFileBytes)
		if err != nil {
			return err
		}
		entry := manifest.Entries[i]
		if entry.Path != source.Path || entry.Size != int64(len(data)) || entry.SHA256 != digest || entry.Executable != executable {
			return fmt.Errorf("hermetic source content changed during capture")
		}
	}
	return nil
}

func sameSelected(a, b []selectedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path {
			return false
		}
	}
	return true
}

type sourceIdentity struct {
	Path       string
	Size       int64
	SHA256     string
	Executable bool
}

func (i sourceIdentity) matches(path string, size int64, digest string, executable bool) bool {
	return i.Path == path && i.Size == size && i.SHA256 == digest && i.Executable == executable
}

func baselineSourceFiles(ctx context.Context, root string, selected []selectedFile, limits core.CaptureLimits) ([]sourceIdentity, error) {
	reader, err := openSourceRoot(root)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	out := make([]sourceIdentity, 0, len(selected))
	total := int64(0)
	for _, source := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, executable, digest, err := reader.ReadRegular(source.Path, limits.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		total += int64(len(data))
		if total > limits.MaxTotalBytes {
			return nil, fmt.Errorf("hermetic capture byte budget exceeded")
		}
		out = append(out, sourceIdentity{Path: source.Path, Size: int64(len(data)), SHA256: digest, Executable: executable})
	}
	return out, nil
}
