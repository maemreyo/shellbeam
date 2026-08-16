package store

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const maxCheckpointRecordBytes int64 = 1 << 20

var storeCheckpointIDPattern = regexp.MustCompile(`^chk_[0-9A-HJKMNP-TV-Z]{26}$`)

func (r *Repository) initCheckpointStore() error {
	for _, dir := range []string{
		r.checkpointBaseRoot(),
		r.checkpointRoot(),
		r.checkpointCreateDir(),
		r.checkpointMetadataDir(),
		r.checkpointRestoreRoot(),
	} {
		if err := ensurePrivateDir(dir); err != nil {
			return fmt.Errorf("checkpoint store: %w", err)
		}
	}
	return nil
}

func verifyCheckpointPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe checkpoint directory")
	}
	return nil
}

func (r *Repository) verifyCheckpointStoreUnlocked() (bool, error) {
	if _, err := os.Lstat(r.checkpointBaseRoot()); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	for _, dir := range []string{r.checkpointBaseRoot(), r.checkpointRoot(), r.checkpointCreateDir(), r.checkpointMetadataDir(), r.checkpointRestoreRoot()} {
		if err := verifyCheckpointPrivateDir(dir); err != nil {
			return false, err
		}
	}
	return true, nil
}

func missingCheckpointDir(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (r *Repository) readCheckpointCreateUnlocked(createID string) (checkpointapp.CreateReservation, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return checkpointapp.CreateReservation{}, err
	}
	if !present {
		return checkpointapp.CreateReservation{}, ErrNotFound
	}
	if _, err := operation.ParseID(createID); err != nil {
		return checkpointapp.CreateReservation{}, fmt.Errorf("invalid checkpoint create id")
	}
	var reservation checkpointapp.CreateReservation
	if err := readPrivateJSON(r.checkpointCreatePath(createID), maxCheckpointRecordBytes, &reservation); err != nil {
		return checkpointapp.CreateReservation{}, err
	}
	if err := reservation.Validate(); err != nil || reservation.CreateID != createID {
		return checkpointapp.CreateReservation{}, fmt.Errorf("invalid checkpoint create reservation")
	}
	return reservation, nil
}

func (r *Repository) readCheckpointMetadataUnlocked(checkpointID string) (core.Checkpoint, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return core.Checkpoint{}, err
	}
	if !present {
		return core.Checkpoint{}, ErrNotFound
	}
	if !storeCheckpointIDPattern.MatchString(checkpointID) {
		return core.Checkpoint{}, fmt.Errorf("invalid checkpoint id")
	}
	var checkpoint core.Checkpoint
	if err := readPrivateJSON(r.checkpointMetadataPath(checkpointID), maxCheckpointRecordBytes, &checkpoint); err != nil {
		return core.Checkpoint{}, err
	}
	if err := checkpoint.Validate(); err != nil || checkpoint.CheckpointID != checkpointID {
		return core.Checkpoint{}, fmt.Errorf("invalid checkpoint metadata")
	}
	return checkpoint, nil
}

func (r *Repository) readCheckpointRestoreReservationUnlocked(restoreID string) (checkpointapp.RestoreReservation, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return checkpointapp.RestoreReservation{}, err
	}
	if !present {
		return checkpointapp.RestoreReservation{}, ErrNotFound
	}
	if err := verifyCheckpointPrivateDir(r.checkpointRestoreDir(restoreID)); err != nil {
		return checkpointapp.RestoreReservation{}, missingCheckpointDir(err)
	}
	if _, err := operation.ParseID(restoreID); err != nil {
		return checkpointapp.RestoreReservation{}, fmt.Errorf("invalid checkpoint restore id")
	}
	var reservation checkpointapp.RestoreReservation
	if err := readPrivateJSON(r.checkpointRestoreReservationPath(restoreID), maxCheckpointRecordBytes, &reservation); err != nil {
		return checkpointapp.RestoreReservation{}, err
	}
	if err := reservation.Validate(); err != nil || reservation.RestoreID != restoreID {
		return checkpointapp.RestoreReservation{}, fmt.Errorf("invalid checkpoint restore reservation")
	}
	return reservation, nil
}

func (r *Repository) readCheckpointRestorePathUnlocked(restoreID string, ordinal int) (core.RestorePathResult, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return core.RestorePathResult{}, err
	}
	if !present {
		return core.RestorePathResult{}, ErrNotFound
	}
	if err := verifyCheckpointPrivateDir(r.checkpointRestoreDir(restoreID)); err != nil {
		return core.RestorePathResult{}, missingCheckpointDir(err)
	}
	if err := verifyCheckpointPrivateDir(r.checkpointRestorePathsDir(restoreID)); err != nil {
		return core.RestorePathResult{}, missingCheckpointDir(err)
	}
	var result core.RestorePathResult
	if err := readPrivateJSON(r.checkpointRestorePathResultPath(restoreID, ordinal), maxCheckpointRecordBytes, &result); err != nil {
		return core.RestorePathResult{}, err
	}
	if err := result.Validate(); err != nil {
		return core.RestorePathResult{}, err
	}
	return result, nil
}

func (r *Repository) readCheckpointRestoreResultUnlocked(restoreID string) (core.RestoreResult, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return core.RestoreResult{}, err
	}
	if !present {
		return core.RestoreResult{}, ErrNotFound
	}
	if err := verifyCheckpointPrivateDir(r.checkpointRestoreDir(restoreID)); err != nil {
		return core.RestoreResult{}, missingCheckpointDir(err)
	}
	var result core.RestoreResult
	if err := readPrivateJSON(r.checkpointRestoreResultPath(restoreID), maxCheckpointRecordBytes, &result); err != nil {
		return core.RestoreResult{}, err
	}
	if err := result.Validate(); err != nil || result.RestoreID != restoreID {
		return core.RestoreResult{}, fmt.Errorf("invalid checkpoint restore result")
	}
	return result, nil
}

func (r *Repository) listCheckpointMetadataUnlocked() ([]core.Checkpoint, error) {
	present, err := r.verifyCheckpointStoreUnlocked()
	if err != nil {
		return nil, err
	}
	if !present {
		return []core.Checkpoint{}, nil
	}
	entries, err := os.ReadDir(r.checkpointMetadataDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > core.MaxRetainedCheckpoints+16 {
		return nil, fmt.Errorf("checkpoint metadata scan limit exceeded")
	}
	out := make([]core.Checkpoint, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe checkpoint metadata entry")
		}
		checkpointID := strings.TrimSuffix(entry.Name(), ".json")
		checkpoint, err := r.readCheckpointMetadataUnlocked(checkpointID)
		if err != nil {
			return nil, err
		}
		out = append(out, checkpoint)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CheckpointID < out[j].CheckpointID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func optionalCheckpoint(value core.Checkpoint, err error) (*core.Checkpoint, error) {
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	copy := value
	return &copy, nil
}
