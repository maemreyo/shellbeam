package localfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"golang.org/x/sys/unix"
)

func (p *Provider) capture(ctx context.Context, request checkpointapp.CaptureRequest) (checkpointapp.CaptureResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	selectors, err := canonicalSelectors(request.Paths)
	if err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	request.Paths = selectors
	if result, complete, err := p.replayCompletedCapture(request); complete || err != nil {
		return result, err
	}
	selected, err := p.selectCapture(ctx, request, defaultSelectionLimits())
	if err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	layout, err := p.ensurePrivateLayout(request.CheckpointID)
	if err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	defer layout.close()
	manifest, err := p.prepareCaptureManifest(layout, request, selected)
	if err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	if err := p.validateCapturedPrefix(layout, manifest, selected); err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	if err := p.captureRemaining(ctx, layout, request.Root, selected, &manifest); err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	if err := p.finalizeCapture(layout, &manifest); err != nil {
		return checkpointapp.CaptureResult{}, err
	}
	return captureResult(manifest), nil
}

func (p *Provider) replayCompletedCapture(request checkpointapp.CaptureRequest) (checkpointapp.CaptureResult, bool, error) {
	existing, err := p.loadManifest(request.CheckpointID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
			return checkpointapp.CaptureResult{}, false, nil
		}
		return checkpointapp.CaptureResult{}, false, err
	}
	if err := manifestMatchesRequest(existing, request); err != nil {
		return checkpointapp.CaptureResult{}, false, err
	}
	if !existing.Complete {
		return checkpointapp.CaptureResult{}, false, nil
	}
	if err := p.validateCompleteManifest(existing); err != nil {
		return checkpointapp.CaptureResult{}, false, err
	}
	return captureResult(existing), true, nil
}

func (p *Provider) prepareCaptureManifest(layout *privateLayout, request checkpointapp.CaptureRequest, selected selectedCapture) (privateManifest, error) {
	manifest, err := p.loadManifestFromLayout(layout)
	if err == nil {
		if err := manifestMatchesRequest(manifest, request); err != nil {
			return privateManifest{}, err
		}
		return manifest, nil
	}
	if !isNotExist(err) {
		return privateManifest{}, err
	}
	manifest = privateManifest{
		SchemaVersion: providerSchemaVersion, CheckpointID: request.CheckpointID, WorkspaceID: request.WorkspaceID,
		RepositoryID: request.RepositoryID, ActivityID: request.ActivityID, Root: request.Root,
		SourceGeneration: request.SourceGeneration, Paths: append([]string(nil), request.Paths...),
		Entries: []privateEntry{}, Excluded: append([]core.PathSummary(nil), selected.Excluded...),
	}
	if err := writePrivateManifest(layout, manifest); err != nil {
		return privateManifest{}, err
	}
	return manifest, nil
}

func (p *Provider) validateCapturedPrefix(layout *privateLayout, manifest privateManifest, selected selectedCapture) error {
	if len(manifest.Entries) > len(selected.Entries) {
		return fmt.Errorf("private manifest exceeds selected entries")
	}
	for i := range manifest.Entries {
		if manifest.Entries[i].Path != selected.Entries[i].Path || manifest.Entries[i].Kind != selected.Entries[i].Kind {
			return fmt.Errorf("partial capture selection changed")
		}
		if err := p.validatePrivateEntry(layout, manifest.Entries[i]); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) captureRemaining(ctx context.Context, layout *privateLayout, root string, selected selectedCapture, manifest *privateManifest) error {
	rootFD, err := openDirNoFollow(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	for i := len(manifest.Entries); i < len(selected.Entries); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := p.captureEntry(layout, rootFD, selected.Entries[i])
		if err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, entry)
		manifest.TotalBytes += entry.Size
		if err := writePrivateManifest(layout, *manifest); err != nil {
			return err
		}
		if p.afterEntry != nil {
			if err := p.afterEntry(len(manifest.Entries)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provider) finalizeCapture(layout *privateLayout, manifest *privateManifest) error {
	manifest.Complete = true
	if err := writePrivateManifest(layout, *manifest); err != nil {
		return err
	}
	marker, err := marshalPrivate(privateComplete{SchemaVersion: providerSchemaVersion})
	if err != nil {
		return err
	}
	if err := privateWriteNewAt(layout.checkpointFD, "complete", marker); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	return p.validateCompleteManifestWithLayout(layout, *manifest)
}

func writePrivateManifest(layout *privateLayout, manifest privateManifest) error {
	raw, err := marshalPrivate(manifest)
	if err != nil {
		return err
	}
	return privateAtomicWriteAt(layout.checkpointFD, "manifest.json", raw)
}

func (p *Provider) loadManifestFromLayout(layout *privateLayout) (privateManifest, error) {
	raw, err := privateReadAt(layout.checkpointFD, "manifest.json", maxPrivateManifestBytes)
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

func manifestMatchesRequest(m privateManifest, r checkpointapp.CaptureRequest) error {
	paths, err := canonicalSelectors(r.Paths)
	if err != nil {
		return err
	}
	if m.CheckpointID != r.CheckpointID || m.WorkspaceID != r.WorkspaceID || m.RepositoryID != r.RepositoryID || m.ActivityID != r.ActivityID || m.Root != r.Root || m.SourceGeneration != r.SourceGeneration || !reflect.DeepEqual(m.Paths, paths) {
		return fmt.Errorf("checkpoint private capture intent conflict")
	}
	return nil
}

func captureResult(m privateManifest) checkpointapp.CaptureResult {
	refs := make([]string, 0, len(m.Entries))
	for _, entry := range m.Entries {
		refs = append(refs, entry.OpaqueRef)
	}
	return checkpointapp.CaptureResult{
		CapturedPathCount: len(m.Entries), Excluded: append([]core.PathSummary(nil), m.Excluded...),
		TotalBytes: m.TotalBytes, CaptureQuality: core.CaptureComplete, OpaqueEntryRefs: refs,
	}
}

func (p *Provider) captureEntry(layout *privateLayout, rootFD int, selected selectedEntry) (privateEntry, error) {
	ref := p.newEntryRef()
	if !safeComponent(ref) {
		return privateEntry{}, fmt.Errorf("invalid opaque entry ref")
	}
	entry := privateEntry{Path: selected.Path, Kind: selected.Kind, OpaqueRef: ref}
	switch selected.Kind {
	case entryFile:
		data, mode, err := readRegularAt(rootFD, selected.Path)
		if err != nil {
			return privateEntry{}, err
		}
		if int64(len(data)) > core.MaxRegularFileBytes {
			return privateEntry{}, fmt.Errorf("regular file exceeds limit")
		}
		if err := privateWriteNewAt(layout.entriesFD, ref+".bin", data); err != nil {
			return privateEntry{}, err
		}
		sum := sha256.Sum256(data)
		entry.PrivateIdentity = hex.EncodeToString(sum[:])
		entry.Size = int64(len(data))
		entry.Mode = mode
	case entrySymlink:
		text, err := readSymlinkAt(rootFD, selected.Path)
		if err != nil {
			return privateEntry{}, err
		}
		raw, err := marshalPrivate(privateSymlink{SchemaVersion: providerSchemaVersion, Text: text})
		if err != nil {
			return privateEntry{}, err
		}
		if err := privateWriteNewAt(layout.symlinksFD, ref+".json", raw); err != nil {
			return privateEntry{}, err
		}
		sum := sha256.Sum256([]byte(text))
		entry.PrivateIdentity = hex.EncodeToString(sum[:])
		entry.Size = int64(len(text))
	case entryAbsent:
		if err := confirmAbsentAt(rootFD, selected.Path); err != nil {
			return privateEntry{}, err
		}
		raw, _ := marshalPrivate(privateAbsent{SchemaVersion: providerSchemaVersion})
		if err := privateWriteNewAt(layout.absentFD, ref+".json", raw); err != nil {
			return privateEntry{}, err
		}
	case entryDirectory:
		if err := confirmDirectoryAt(rootFD, selected.Path); err != nil {
			return privateEntry{}, err
		}
	default:
		return privateEntry{}, fmt.Errorf("unsupported private entry kind")
	}
	return entry, nil
}

func readRegularAt(rootFD int, rel string) ([]byte, uint32, error) {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		return nil, 0, err
	}
	defer unix.Close(parent)
	before, err := statAtNoFollow(parent, name)
	if err != nil {
		return nil, 0, err
	}
	if fileType(before) != unix.S_IFREG {
		return nil, 0, fmt.Errorf("selected file type changed")
	}
	if before.Size > core.MaxRegularFileBytes {
		return nil, 0, fmt.Errorf("regular file exceeds limit")
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, err
	}
	file := os.NewFile(uintptr(fd), rel)
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("open selected file")
	}
	defer file.Close()
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, 0, err
	}
	if fileType(after) != unix.S_IFREG || before.Dev != after.Dev || before.Ino != after.Ino {
		return nil, 0, fmt.Errorf("selected file changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, core.MaxRegularFileBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(data)) > core.MaxRegularFileBytes {
		return nil, 0, fmt.Errorf("regular file exceeds limit")
	}
	return data, uint32(after.Mode & 0777), nil
}

func readSymlinkAt(rootFD int, rel string) (string, error) {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		return "", err
	}
	defer unix.Close(parent)
	st, err := statAtNoFollow(parent, name)
	if err != nil {
		return "", err
	}
	if fileType(st) != unix.S_IFLNK {
		return "", fmt.Errorf("selected symlink changed")
	}
	return readlinkAt(parent, name)
}

func confirmAbsentAt(rootFD int, rel string) error {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	defer unix.Close(parent)
	_, err = statAtNoFollow(parent, name)
	if isNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("selected absent path now exists")
}

func confirmDirectoryAt(rootFD int, rel string) error {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	st, err := statAtNoFollow(parent, name)
	if err != nil {
		return err
	}
	if fileType(st) != unix.S_IFDIR {
		return fmt.Errorf("selected directory changed")
	}
	return nil
}

func (p *Provider) validateCompleteManifest(m privateManifest) error {
	l, err := p.openPrivateLayout(m.CheckpointID)
	if err != nil {
		return err
	}
	defer l.close()
	return p.validateCompleteManifestWithLayout(l, m)
}

func (p *Provider) validateCompleteManifestWithLayout(l *privateLayout, m privateManifest) error {
	marker, err := privateReadAt(l.checkpointFD, "complete", 1024)
	if err != nil {
		return err
	}
	var complete privateComplete
	if err := strictJSON(marker, &complete); err != nil || complete.SchemaVersion != providerSchemaVersion {
		return fmt.Errorf("invalid capture complete marker")
	}
	for _, entry := range m.Entries {
		if err := p.validatePrivateEntry(l, entry); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) validatePrivateEntry(l *privateLayout, entry privateEntry) error {
	switch entry.Kind {
	case entryFile:
		raw, err := privateReadAt(l.entriesFD, entry.OpaqueRef+".bin", core.MaxRegularFileBytes)
		if err != nil {
			return err
		}
		if int64(len(raw)) != entry.Size {
			return fmt.Errorf("private file size mismatch")
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != entry.PrivateIdentity {
			return fmt.Errorf("private file identity mismatch")
		}
	case entrySymlink:
		raw, err := privateReadAt(l.symlinksFD, entry.OpaqueRef+".json", 128<<10)
		if err != nil {
			return err
		}
		var value privateSymlink
		if err := strictJSON(raw, &value); err != nil || value.SchemaVersion != providerSchemaVersion {
			return fmt.Errorf("invalid private symlink")
		}
		if int64(len(value.Text)) != entry.Size {
			return fmt.Errorf("private symlink size mismatch")
		}
		sum := sha256.Sum256([]byte(value.Text))
		if hex.EncodeToString(sum[:]) != entry.PrivateIdentity {
			return fmt.Errorf("private symlink identity mismatch")
		}
	case entryAbsent:
		raw, err := privateReadAt(l.absentFD, entry.OpaqueRef+".json", 1024)
		if err != nil {
			return err
		}
		var value privateAbsent
		if err := strictJSON(raw, &value); err != nil || value.SchemaVersion != providerSchemaVersion {
			return fmt.Errorf("invalid private absent marker")
		}
	case entryDirectory:
		return nil
	default:
		return fmt.Errorf("invalid private entry kind")
	}
	return nil
}
