package sourcefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	app "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Binder struct {
	retention      app.SourceRetention
	maxSourceBytes int64
	afterRead      func()
}

func NewBinder(retention app.SourceRetention, maxSourceBytes int64) (*Binder, error) {
	if retention == nil || maxSourceBytes < 1 {
		return nil, fmt.Errorf("invalid source binder config")
	}
	return &Binder{retention: retention, maxSourceBytes: maxSourceBytes}, nil
}

func (b *Binder) Bind(ctx context.Context, boundWorkspace workspace.Workspace, logicalPath string) (app.BoundSource, error) {
	if err := ctx.Err(); err != nil {
		return app.BoundSource{}, err
	}
	if err := boundWorkspace.Validate(); err != nil {
		return app.BoundSource{}, sourceUnavailable(err)
	}
	if !safeLogicalPath(logicalPath) {
		return app.BoundSource{}, &app.Error{Code: app.CodePositionInvalid}
	}

	file, identity, err := openRegularNoFollow(boundWorkspace.Root, logicalPath, b.maxSourceBytes)
	if err != nil {
		return app.BoundSource{}, err
	}
	defer file.Close()

	before, err := file.Stat()
	if err != nil {
		return app.BoundSource{}, sourceUnavailable(err)
	}
	data, err := io.ReadAll(io.LimitReader(file, b.maxSourceBytes+1))
	if err != nil {
		return app.BoundSource{}, sourceUnavailable(err)
	}
	if int64(len(data)) > b.maxSourceBytes {
		return app.BoundSource{}, &app.Error{Code: app.CodeQueryBudgetExceeded}
	}
	if b.afterRead != nil {
		b.afterRead()
	}
	if err := verifyStableFile(file, before); err != nil {
		return app.BoundSource{}, err
	}
	if err := verifyCurrentIdentity(boundWorkspace.Root, logicalPath, identity); err != nil {
		return app.BoundSource{}, err
	}
	if !utf8.Valid(data) {
		return app.BoundSource{}, &app.Error{Code: app.CodeUnsupportedEncoding}
	}
	if err := ctx.Err(); err != nil {
		return app.BoundSource{}, err
	}

	ref := core.SourceRef{
		Origin:            core.SourceWorkspace,
		RepositoryID:      boundWorkspace.RepositoryID,
		WorkspaceID:       boundWorkspace.ID,
		LogicalPath:       logicalPath,
		DisplayIdentity:   logicalPath,
		ResolutionQuality: core.ResolutionExact,
		TextEncoding:      core.TextEncodingUTF8,
	}
	return b.retention.Retain(ref, data)
}

func (b *Binder) Resolve(id core.SourceRefID) (app.BoundSource, app.SourceRefState) {
	return b.retention.Resolve(id)
}

func openRegularNoFollow(root, logicalPath string, maxBytes int64) (*os.File, unix.Stat_t, error) {
	parentFD, name, err := openParentNoFollow(root, logicalPath)
	if err != nil {
		return nil, unix.Stat_t{}, sourceUnavailable(err)
	}
	defer unix.Close(parentFD)

	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, unix.Stat_t{}, sourceUnavailable(err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, unix.Stat_t{}, sourceUnavailable(fmt.Errorf("source is not a regular file"))
	}
	if before.Size > maxBytes {
		return nil, unix.Stat_t{}, &app.Error{Code: app.CodeQueryBudgetExceeded}
	}

	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, unix.Stat_t{}, sourceUnavailable(err)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, sourceUnavailable(err)
	}
	if !sameIdentity(before, opened) || opened.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, sourceChanged(fmt.Errorf("source changed while opening"))
	}
	file := os.NewFile(uintptr(fd), logicalPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, sourceUnavailable(fmt.Errorf("create source file handle"))
	}
	return file, opened, nil
}

func openParentNoFollow(root, logicalPath string) (int, string, error) {
	parts := strings.Split(logicalPath, "/")
	if len(parts) == 0 {
		return -1, "", fmt.Errorf("empty source path")
	}
	dirFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", err
	}
	for _, component := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(dirFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		_ = unix.Close(dirFD)
		if openErr != nil {
			return -1, "", openErr
		}
		dirFD = next
	}
	return dirFD, parts[len(parts)-1], nil
}

func verifyStableFile(file *os.File, before os.FileInfo) error {
	after, err := file.Stat()
	if err != nil {
		return sourceUnavailable(err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return sourceChanged(fmt.Errorf("source changed while reading"))
	}
	return nil
}

func verifyCurrentIdentity(root, logicalPath string, expected unix.Stat_t) error {
	parentFD, name, err := openParentNoFollow(root, logicalPath)
	if err != nil {
		return sourceChanged(err)
	}
	defer unix.Close(parentFD)
	var current unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return sourceChanged(err)
	}
	if current.Mode&unix.S_IFMT != unix.S_IFREG || !sameIdentity(expected, current) {
		return sourceChanged(fmt.Errorf("source pathname changed after read"))
	}
	return nil
}

func sameIdentity(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino
}

func safeLogicalPath(value string) bool {
	if value == "" || len(value) > core.MaxLogicalPathBytes || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func sourceUnavailable(cause error) error {
	return &app.Error{Code: app.CodeSourceRefUnavailable, Cause: cause}
}

func sourceChanged(cause error) error {
	return &app.Error{Code: app.CodeSourceChanged, Retryable: true, Cause: cause}
}
