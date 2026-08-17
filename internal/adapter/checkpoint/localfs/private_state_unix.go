package localfs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/unix"
)

const maxPrivateManifestBytes = 4 << 20

type privateLayout struct {
	stateFD       int
	contentFD     int
	versionFD     int
	checkpointsFD int
	checkpointFD  int
	entriesFD     int
	symlinksFD    int
	absentFD      int
}

func (l *privateLayout) close() {
	for _, fd := range []int{l.absentFD, l.symlinksFD, l.entriesFD, l.checkpointFD, l.checkpointsFD, l.versionFD, l.contentFD, l.stateFD} {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}

func invalidLayout() *privateLayout {
	return &privateLayout{stateFD: -1, contentFD: -1, versionFD: -1, checkpointsFD: -1, checkpointFD: -1, entriesFD: -1, symlinksFD: -1, absentFD: -1}
}

func validatePrivateDirFD(fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0777 != 0700 || st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe private directory")
	}
	return nil
}

func validatePrivateRegularFD(fd int, minSize, maxSize int64) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Size < minSize || st.Size > maxSize {
		return st, fmt.Errorf("unsafe private file")
	}
	return st, nil
}

func openPrivateRoot(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if err := validatePrivateDirFD(fd); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func ensurePrivateDirAt(parent int, name string) (int, error) {
	if !safeComponent(name) {
		return -1, fmt.Errorf("unsafe private directory name")
	}
	if err := unix.Mkdirat(parent, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, err
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if err := validatePrivateDirFD(fd); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func openPrivateDirAt(parent int, name string) (int, error) {
	if !safeComponent(name) {
		return -1, fmt.Errorf("unsafe private directory name")
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if err := validatePrivateDirFD(fd); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func (p *Provider) ensurePrivateLayout(checkpointID string) (*privateLayout, error) {
	if !safeComponent(checkpointID) {
		return nil, fmt.Errorf("invalid checkpoint id")
	}
	l := invalidLayout()
	var err error
	if l.stateFD, err = openPrivateRoot(p.stateDir); err != nil {
		return nil, err
	}
	fail := func(e error) (*privateLayout, error) { l.close(); return nil, e }
	if l.contentFD, err = ensurePrivateDirAt(l.stateFD, "checkpoint-content"); err != nil {
		return fail(err)
	}
	if l.versionFD, err = ensurePrivateDirAt(l.contentFD, "v1"); err != nil {
		return fail(err)
	}
	if l.checkpointsFD, err = ensurePrivateDirAt(l.versionFD, "checkpoints"); err != nil {
		return fail(err)
	}
	if l.checkpointFD, err = ensurePrivateDirAt(l.checkpointsFD, checkpointID); err != nil {
		return fail(err)
	}
	if l.entriesFD, err = ensurePrivateDirAt(l.checkpointFD, "entries"); err != nil {
		return fail(err)
	}
	if l.symlinksFD, err = ensurePrivateDirAt(l.checkpointFD, "symlinks"); err != nil {
		return fail(err)
	}
	if l.absentFD, err = ensurePrivateDirAt(l.checkpointFD, "absent"); err != nil {
		return fail(err)
	}
	return l, nil
}

func (p *Provider) openPrivateLayout(checkpointID string) (*privateLayout, error) {
	if !safeComponent(checkpointID) {
		return nil, fmt.Errorf("invalid checkpoint id")
	}
	l := invalidLayout()
	var err error
	if l.stateFD, err = openPrivateRoot(p.stateDir); err != nil {
		return nil, err
	}
	fail := func(e error) (*privateLayout, error) { l.close(); return nil, e }
	if l.contentFD, err = openPrivateDirAt(l.stateFD, "checkpoint-content"); err != nil {
		return fail(err)
	}
	if l.versionFD, err = openPrivateDirAt(l.contentFD, "v1"); err != nil {
		return fail(err)
	}
	if l.checkpointsFD, err = openPrivateDirAt(l.versionFD, "checkpoints"); err != nil {
		return fail(err)
	}
	if l.checkpointFD, err = openPrivateDirAt(l.checkpointsFD, checkpointID); err != nil {
		return fail(err)
	}
	if l.entriesFD, err = openPrivateDirAt(l.checkpointFD, "entries"); err != nil {
		return fail(err)
	}
	if l.symlinksFD, err = openPrivateDirAt(l.checkpointFD, "symlinks"); err != nil {
		return fail(err)
	}
	if l.absentFD, err = openPrivateDirAt(l.checkpointFD, "absent"); err != nil {
		return fail(err)
	}
	return l, nil
}

func privateReadAt(dirFD int, name string, maxBytes int64) ([]byte, error) {
	if !safeComponent(name) {
		return nil, fmt.Errorf("invalid private file name")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open private file")
	}
	defer file.Close()
	if _, err := validatePrivateRegularFD(fd, 0, maxBytes); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("private file exceeds limit")
	}
	return raw, nil
}

func privateWriteNewAt(dirFD int, name string, raw []byte) error {
	if !safeComponent(name) {
		return fmt.Errorf("invalid private file name")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open private file")
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = unix.Unlinkat(dirFD, name, 0)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return unix.Fsync(dirFD)
}

func privateAtomicWriteAt(dirFD int, name string, raw []byte) error {
	if !safeComponent(name) {
		return fmt.Errorf("invalid private file name")
	}
	tmp := ".tmp-" + ulid.Make().String()
	fd, err := unix.Openat(dirFD, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmp)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open private temp")
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = unix.Unlinkat(dirFD, tmp, 0)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(dirFD, tmp, dirFD, name); err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(dirFD)
}

func strictJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("trailing private json")
	}
	return nil
}

func marshalPrivate(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (p *Provider) loadManifest(checkpointID string) (privateManifest, error) {
	l, err := p.openPrivateLayout(checkpointID)
	if err != nil {
		return privateManifest{}, err
	}
	defer l.close()
	raw, err := privateReadAt(l.checkpointFD, "manifest.json", maxPrivateManifestBytes)
	if err != nil {
		return privateManifest{}, err
	}
	var manifest privateManifest
	if err := strictJSON(raw, &manifest); err != nil {
		return privateManifest{}, err
	}
	if err := validatePrivateManifest(manifest); err != nil {
		return privateManifest{}, err
	}
	return manifest, nil
}

func validPrivateRetentionState(state core.RetentionState) bool {
	switch state {
	case core.RetentionAvailable, core.RetentionPartiallyCompacted, core.RetentionExpired:
		return true
	default:
		return false
	}
}

func validatePrivateManifest(m privateManifest) error {
	if m.SchemaVersion != providerSchemaVersion || !safeComponent(m.CheckpointID) || m.WorkspaceID == "" || m.RepositoryID == "" || !filepath.IsAbs(m.Root) || m.SourceGeneration == "" || m.CreatedAt.IsZero() || !validPrivateRetentionState(m.RetentionState) || len(m.Paths) < 1 || len(m.Paths) > 32 || m.TotalBytes < 0 {
		return fmt.Errorf("invalid private checkpoint manifest")
	}
	selectors, err := canonicalSelectors(m.Paths)
	if err != nil {
		return err
	}
	if len(selectors) != len(m.Paths) {
		return fmt.Errorf("invalid private checkpoint manifest")
	}
	for i := range selectors {
		if selectors[i] != m.Paths[i] {
			return fmt.Errorf("non-canonical private checkpoint manifest")
		}
	}
	seenPath := map[string]struct{}{}
	seenRef := map[string]struct{}{}
	for _, entry := range m.Entries {
		if entry.Path == "" || !safeComponent(entry.OpaqueRef) || entry.Size < 0 {
			return fmt.Errorf("invalid private checkpoint entry")
		}
		switch entry.Kind {
		case entryFile, entryDirectory, entrySymlink, entryAbsent:
		default:
			return fmt.Errorf("invalid private checkpoint entry kind")
		}
		if _, ok := seenPath[entry.Path]; ok {
			return fmt.Errorf("duplicate private checkpoint path")
		}
		if _, ok := seenRef[entry.OpaqueRef]; ok {
			return fmt.Errorf("duplicate private checkpoint ref")
		}
		seenPath[entry.Path] = struct{}{}
		seenRef[entry.OpaqueRef] = struct{}{}
	}
	return nil
}
