package store

import (
	"fmt"
	"path/filepath"
)

func (r *Repository) checkpointBaseRoot() string {
	return filepath.Join(r.root, "checkpoints")
}

func (r *Repository) checkpointRoot() string {
	return filepath.Join(r.checkpointBaseRoot(), "v1")
}

func (r *Repository) checkpointCreateDir() string {
	return filepath.Join(r.checkpointRoot(), "create")
}

func (r *Repository) checkpointMetadataDir() string {
	return filepath.Join(r.checkpointRoot(), "by-id")
}

func (r *Repository) checkpointRestoreRoot() string {
	return filepath.Join(r.checkpointRoot(), "restore")
}

func (r *Repository) checkpointCreatePath(createID string) string {
	return filepath.Join(r.checkpointCreateDir(), createID+".json")
}

func (r *Repository) checkpointMetadataPath(checkpointID string) string {
	return filepath.Join(r.checkpointMetadataDir(), checkpointID+".json")
}

func (r *Repository) checkpointRestoreDir(restoreID string) string {
	return filepath.Join(r.checkpointRestoreRoot(), restoreID)
}

func (r *Repository) checkpointRestorePathsDir(restoreID string) string {
	return filepath.Join(r.checkpointRestoreDir(restoreID), "paths")
}

func (r *Repository) checkpointRestoreReservationPath(restoreID string) string {
	return filepath.Join(r.checkpointRestoreDir(restoreID), "reservation.json")
}

func (r *Repository) checkpointRestoreResultPath(restoreID string) string {
	return filepath.Join(r.checkpointRestoreDir(restoreID), "result.json")
}

func (r *Repository) checkpointRestorePathResultPath(restoreID string, ordinal int) string {
	return filepath.Join(r.checkpointRestorePathsDir(restoreID), fmt.Sprintf("%06d.json", ordinal))
}
