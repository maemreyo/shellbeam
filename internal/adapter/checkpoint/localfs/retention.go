package localfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"golang.org/x/sys/unix"
)

type retentionCandidate struct {
	manifest privateManifest
	pinned   bool
}

func (p *Provider) inspect(ctx context.Context, checkpointID string) (checkpointapp.ProviderCheckpointStatus, error) {
	if err := ctx.Err(); err != nil {
		return checkpointapp.ProviderCheckpointStatus{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	manifest, err := p.loadManifest(checkpointID)
	if err != nil {
		return checkpointapp.ProviderCheckpointStatus{}, err
	}
	return checkpointapp.ProviderCheckpointStatus{
		CheckpointID:   manifest.CheckpointID,
		RetentionState: manifest.RetentionState,
		Available:      manifest.RetentionState != core.RetentionExpired,
	}, nil
}

func (p *Provider) sweep(ctx context.Context, request checkpointapp.SweepRequest) (checkpointapp.SweepResult, error) {
	if err := validateSweepRequest(request); err != nil {
		return checkpointapp.SweepResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	candidates, err := p.retentionCandidates(ctx)
	if err != nil {
		return checkpointapp.SweepResult{}, err
	}
	selected := selectRetentionExpirations(candidates, request)
	result := checkpointapp.SweepResult{}
	for _, candidate := range selected {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		freed, err := p.expireCheckpoint(candidate.manifest)
		if err != nil {
			return result, err
		}
		result.ExpiredCheckpointIDs = append(result.ExpiredCheckpointIDs, candidate.manifest.CheckpointID)
		result.FreedBytes += freed
	}
	return result, nil
}

func validateSweepRequest(request checkpointapp.SweepRequest) error {
	if request.Now.IsZero() || request.MaxCheckpoints < 0 || request.MaxBytes < 0 || request.MaxAge < 0 {
		return fmt.Errorf("invalid checkpoint sweep request")
	}
	return nil
}

func (p *Provider) retentionCandidates(ctx context.Context) ([]retentionCandidate, error) {
	stateFD, err := openPrivateRoot(p.stateDir)
	if err != nil {
		return nil, err
	}
	defer unix.Close(stateFD)
	contentFD, err := openPrivateDirAt(stateFD, "checkpoint-content")
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(contentFD)
	versionFD, err := openPrivateDirAt(contentFD, "v1")
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(versionFD)
	checkpointsFD, err := openPrivateDirAt(versionFD, "checkpoints")
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(checkpointsFD)

	names, err := privateDirNames(checkpointsFD)
	if err != nil {
		return nil, err
	}
	out := make([]retentionCandidate, 0, len(names))
	for _, id := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		manifest, err := p.loadManifest(id)
		if err != nil {
			return nil, err
		}
		pinned := !manifest.Complete
		if !pinned {
			pinned, err = p.hasIncompleteRestore(id)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, retentionCandidate{manifest: manifest, pinned: pinned})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].manifest.CreatedAt.Equal(out[j].manifest.CreatedAt) {
			return out[i].manifest.CheckpointID < out[j].manifest.CheckpointID
		}
		return out[i].manifest.CreatedAt.Before(out[j].manifest.CreatedAt)
	})
	return out, nil
}

func selectRetentionExpirations(all []retentionCandidate, request checkpointapp.SweepRequest) []retentionCandidate {
	selected := make([]retentionCandidate, 0)
	chosen := make(map[string]struct{})
	retainedCount := 0
	var retainedBytes int64
	for _, candidate := range all {
		if candidate.manifest.RetentionState == core.RetentionExpired {
			continue
		}
		retainedCount++
		retainedBytes += candidate.manifest.TotalBytes
		if candidate.pinned {
			continue
		}
		if candidate.manifest.RetentionState == core.RetentionPartiallyCompacted ||
			(request.MaxAge > 0 && request.Now.Sub(candidate.manifest.CreatedAt) > request.MaxAge) {
			selected = append(selected, candidate)
			chosen[candidate.manifest.CheckpointID] = struct{}{}
			retainedCount--
			retainedBytes -= candidate.manifest.TotalBytes
		}
	}
	for _, candidate := range all {
		if retainedCount <= request.MaxCheckpoints && retainedBytes <= request.MaxBytes {
			break
		}
		if candidate.pinned || candidate.manifest.RetentionState == core.RetentionExpired {
			continue
		}
		if _, ok := chosen[candidate.manifest.CheckpointID]; ok {
			continue
		}
		selected = append(selected, candidate)
		chosen[candidate.manifest.CheckpointID] = struct{}{}
		retainedCount--
		retainedBytes -= candidate.manifest.TotalBytes
	}
	return selected
}

func (p *Provider) expireCheckpoint(manifest privateManifest) (int64, error) {
	layout, err := p.openPrivateLayout(manifest.CheckpointID)
	if err != nil {
		return 0, err
	}
	defer layout.close()
	if manifest.RetentionState == core.RetentionExpired {
		return 0, nil
	}
	if manifest.RetentionState != core.RetentionPartiallyCompacted {
		manifest.RetentionState = core.RetentionPartiallyCompacted
		if err := writePrivateManifest(layout, manifest); err != nil {
			return 0, err
		}
	}
	if p.beforeRetentionCleanup != nil {
		if err := p.beforeRetentionCleanup(manifest.CheckpointID); err != nil {
			return 0, err
		}
	}
	for _, fd := range []int{layout.entriesFD, layout.symlinksFD, layout.absentFD} {
		if err := clearPrivateFlatDir(fd); err != nil {
			return 0, err
		}
	}
	manifest.RetentionState = core.RetentionExpired
	if err := writePrivateManifest(layout, manifest); err != nil {
		return 0, err
	}
	return manifest.TotalBytes, nil
}

func clearPrivateFlatDir(dirFD int) error {
	dup, err := dupFD(dirFD)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(dup), "private-retention")
	if file == nil {
		_ = unix.Close(dup)
		return fmt.Errorf("open private retention directory")
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !safeComponent(name) {
			return fmt.Errorf("invalid private retention entry")
		}
		st, err := statAtNoFollow(dirFD, name)
		if err != nil {
			return err
		}
		if fileType(st) != unix.S_IFREG {
			return fmt.Errorf("unexpected private retention entry type")
		}
		if err := unix.Unlinkat(dirFD, name, 0); err != nil {
			return err
		}
	}
	return unix.Fsync(dirFD)
}

func privateDirNames(dirFD int) ([]string, error) {
	dup, err := dupFD(dirFD)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(dup), "private-directories")
	if file == nil {
		_ = unix.Close(dup)
		return nil, fmt.Errorf("open private directory list")
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !safeComponent(entry.Name()) {
			return nil, fmt.Errorf("invalid private directory entry")
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (p *Provider) hasIncompleteRestore(checkpointID string) (bool, error) {
	layout, err := p.openPrivateLayout(checkpointID)
	if err != nil {
		return false, err
	}
	defer layout.close()
	restoresFD, err := openPrivateDirAt(layout.checkpointFD, "restores")
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(restoresFD)
	names, err := privateDirNames(restoresFD)
	if err != nil {
		return false, err
	}
	for _, restoreID := range names {
		restoreFD, err := openPrivateDirAt(restoresFD, restoreID)
		if err != nil {
			return true, nil
		}
		claimRaw, err := privateReadAt(restoreFD, "claim.json", maxPrivateRestoreRecordBytes)
		if err != nil {
			_ = unix.Close(restoreFD)
			return true, nil
		}
		var claim privateRestoreClaim
		if strictJSON(claimRaw, &claim) != nil || validateRestoreClaim(claim) != nil {
			_ = unix.Close(restoreFD)
			return true, nil
		}
		pathsFD, err := openPrivateDirAt(restoreFD, "paths")
		_ = unix.Close(restoreFD)
		if err != nil {
			return true, nil
		}
		complete := true
		for ordinal := range claim.Paths {
			name := fmt.Sprintf("%06d.json", ordinal)
			raw, readErr := privateReadAt(pathsFD, name, 16<<10)
			if readErr != nil {
				complete = false
				break
			}
			var record privateRestorePathRecord
			if strictJSON(raw, &record) != nil || record.SchemaVersion != providerSchemaVersion || record.Result.Validate() != nil {
				complete = false
				break
			}
		}
		_ = unix.Close(pathsFD)
		if !complete {
			return true, nil
		}
	}
	return false, nil
}
