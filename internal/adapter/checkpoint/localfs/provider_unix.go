package localfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"golang.org/x/sys/unix"
)

func validateSelector(selector string) error {
	if selector == "**" || selector == "" || filepath.IsAbs(selector) || strings.Contains(selector, `\`) {
		return checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "paths", "reason": "selector_invalid"}, nil)
	}
	base := strings.TrimSuffix(selector, "/**")
	if base == "" || strings.ContainsAny(base, "*?[]{}") || strings.HasSuffix(base, "/") {
		return checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "paths", "reason": "selector_invalid"}, nil)
	}
	for _, part := range strings.Split(base, "/") {
		if part == "" || part == "." || part == ".." {
			return checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "paths", "reason": "selector_invalid"}, nil)
		}
	}
	return nil
}

func checkpointFailure(code failure.Code, details map[string]string, cause error) error {
	return failure.New(code, details, cause)
}

func safeComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}

func openDirNoFollow(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func dupFD(fd int) (int, error) {
	copy, err := unix.Dup(fd)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(copy)
	return copy, nil
}

func openParentAt(rootFD int, rel string) (int, string, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || !safeComponent(parts[len(parts)-1]) {
		return -1, "", fmt.Errorf("invalid path")
	}
	fd, err := dupFD(rootFD)
	if err != nil {
		return -1, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		if !safeComponent(part) {
			_ = unix.Close(fd)
			return -1, "", fmt.Errorf("invalid path")
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, "", openErr
		}
		fd = next
	}
	return fd, parts[len(parts)-1], nil
}

func statAtNoFollow(parentFD int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(parentFD, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

func fileType(st unix.Stat_t) uint32 { return uint32(st.Mode) & uint32(unix.S_IFMT) }

func isNotExist(err error) bool { return errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist) }
