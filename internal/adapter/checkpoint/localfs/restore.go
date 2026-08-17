package localfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/unix"
)

func (p *Provider) restore(
	ctx context.Context,
	request checkpointapp.ProviderRestoreRequest,
) (checkpointapp.ProviderRestoreResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	normalized, manifest, entries, err := p.prepareRestoreRequest(request)
	if err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}
	layout, err := p.ensureRestoreLayout(normalized.CheckpointID, normalized.RestoreID)
	if err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}
	defer layout.close()
	if _, err := p.loadOrCreateRestoreClaim(layout, normalized); err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}

	rootFD, err := openDirNoFollow(normalized.Root)
	if err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}
	defer unix.Close(rootFD)
	for _, path := range normalized.Paths {
		if err := rejectSubmoduleCrossing(rootFD, path); err != nil {
			return checkpointapp.ProviderRestoreResult{}, err
		}
	}
	observations, err := p.loadOrCreateRestoreObservations(layout, rootFD, normalized.Paths)
	if err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}
	results, err := p.restorePathLoop(ctx, layout, rootFD, entries, observations)
	if err != nil {
		return checkpointapp.ProviderRestoreResult{}, err
	}
	_ = manifest
	return checkpointapp.ProviderRestoreResult{Paths: results}, nil
}

func (p *Provider) prepareRestoreRequest(
	request checkpointapp.ProviderRestoreRequest,
) (checkpointapp.ProviderRestoreRequest, privateManifest, []privateEntry, error) {
	coreRequest, err := (core.RestoreRequest{
		RestoreID: request.RestoreID, CheckpointID: request.CheckpointID, Paths: request.Paths,
	}).Normalize()
	if err != nil {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, err
	}
	request.Paths = coreRequest.Paths
	request.Root = filepath.Clean(request.Root)
	if request.WorkspaceID == "" || !filepath.IsAbs(request.Root) {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil,
			failure.New(failure.CheckpointScopeInvalid, map[string]string{
				"field": "workspace_id", "reason": "restore_scope_invalid",
			}, nil)
	}
	manifest, err := p.loadManifest(request.CheckpointID)
	if err != nil {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, err
	}
	if manifest.RetentionState == core.RetentionExpired {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, failure.New(
			failure.CheckpointExpired, map[string]string{"checkpoint_id": request.CheckpointID}, nil,
		)
	}
	if manifest.RetentionState != core.RetentionAvailable {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, failure.New(
			failure.CheckpointProviderUnavailable, map[string]string{"provider": p.Identity().ID, "reason": "checkpoint_compacted"}, nil,
		)
	}
	if !manifest.Complete || manifest.WorkspaceID != request.WorkspaceID {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil,
			restoreRequestConflict(request.RestoreID)
	}
	if err := p.validateCompleteManifest(manifest); err != nil {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, err
	}
	entries, err := entriesForRestore(manifest, request.Paths)
	if err != nil {
		return checkpointapp.ProviderRestoreRequest{}, privateManifest{}, nil, err
	}
	return request, manifest, entries, nil
}

func entriesForRestore(manifest privateManifest, paths []string) ([]privateEntry, error) {
	byPath := make(map[string]privateEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		byPath[entry.Path] = entry
	}
	out := make([]privateEntry, 0, len(paths))
	for _, path := range paths {
		entry, ok := byPath[path]
		if !ok {
			return nil, failure.New(
				failure.CheckpointPathUnsupported,
				map[string]string{"path": path, "reason": "not_captured"},
				nil,
			)
		}
		out = append(out, entry)
	}
	return out, nil
}

func (p *Provider) restorePathLoop(
	ctx context.Context,
	layout *restoreLayout,
	rootFD int,
	entries []privateEntry,
	observations []privateObservation,
) ([]core.RestorePathResult, error) {
	results := make([]core.RestorePathResult, 0, len(entries))
	for ordinal, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if existing, found, err := loadRestorePathResult(layout, ordinal); err != nil {
			return nil, err
		} else if found {
			if existing.Path != entry.Path {
				return nil, fmt.Errorf("private restore path replay mismatch")
			}
			results = append(results, existing)
			continue
		}
		result, err := p.restoreOnePath(layout, rootFD, entry, observations[ordinal])
		if err != nil {
			return nil, err
		}
		if err := storeRestorePathResult(layout, ordinal, result); err != nil {
			return nil, err
		}
		results = append(results, result)
		if p.afterRestorePath != nil {
			if err := p.afterRestorePath(ordinal); err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

func (p *Provider) restoreOnePath(
	layout *restoreLayout,
	rootFD int,
	entry privateEntry,
	expected privateObservation,
) (core.RestorePathResult, error) {
	desired, err := desiredObservation(entry)
	if err != nil {
		return core.RestorePathResult{}, err
	}
	if entry.Kind == entryDirectory {
		return restorePathResult(entry.Path, core.RestoreUnsupported, "directory_tree"), nil
	}
	if sameObservation(expected, desired) {
		return restorePathResult(entry.Path, core.RestoreNoop, ""), nil
	}
	if observationMutationUnsupported(expected) {
		return restorePathResult(entry.Path, core.RestoreUnsupported, "current_unsupported"), nil
	}
	if p.beforeRestoreMutation != nil {
		p.beforeRestoreMutation(entry.Path)
	}
	current, err := observePath(rootFD, entry.Path)
	if err != nil {
		return core.RestorePathResult{}, err
	}
	if !sameObservation(current, expected) {
		return restorePathResult(entry.Path, core.RestoreConflict, "current_changed"), nil
	}
	if err := p.applyCapturedState(layout, rootFD, entry); err != nil {
		return core.RestorePathResult{}, err
	}
	return restorePathResult(entry.Path, core.RestoreRestored, ""), nil
}

func (p *Provider) applyCapturedState(
	layout *restoreLayout,
	rootFD int,
	entry privateEntry,
) error {
	switch entry.Kind {
	case entryFile:
		raw, err := privateReadAt(layout.capture.entriesFD, entry.OpaqueRef+".bin", core.MaxRegularFileBytes)
		if err != nil {
			return err
		}
		return restoreRegularAt(rootFD, entry.Path, raw, entry.Mode)
	case entrySymlink:
		raw, err := privateReadAt(layout.capture.symlinksFD, entry.OpaqueRef+".json", 128<<10)
		if err != nil {
			return err
		}
		var value privateSymlink
		if err := strictJSON(raw, &value); err != nil || value.SchemaVersion != providerSchemaVersion {
			return fmt.Errorf("invalid private symlink")
		}
		return restoreSymlinkAt(rootFD, entry.Path, value.Text)
	case entryAbsent:
		return restoreAbsentAt(rootFD, entry.Path)
	default:
		return fmt.Errorf("unsupported restore mutation kind")
	}
}

func restoreRegularAt(rootFD int, rel string, data []byte, mode uint32) error {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	tmp := ".shellbeam-restore-" + ulid.Make().String()
	fd, err := unix.Openat(parent, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmp)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open restore staging file")
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = unix.Unlinkat(parent, tmp, 0)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(os.FileMode(mode & 0777)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(parent, tmp, parent, name); err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(parent)
}

func restoreSymlinkAt(rootFD int, rel, text string) error {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	tmp := ".shellbeam-restore-" + ulid.Make().String()
	if err := unix.Symlinkat(text, parent, tmp); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(parent, tmp, 0)
		}
	}()
	if err := unix.Renameat(parent, tmp, parent, name); err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(parent)
}

func restoreAbsentAt(rootFD int, rel string) error {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	defer unix.Close(parent)
	if err := unix.Unlinkat(parent, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return unix.Fsync(parent)
}

func restorePathResult(path string, outcome core.RestorePathOutcome, reason string) core.RestorePathResult {
	return core.RestorePathResult{Path: path, Outcome: outcome, Reason: reason}
}
