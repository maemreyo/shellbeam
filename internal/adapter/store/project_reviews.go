package store

import (
	"context"
	"errors"
	"path/filepath"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) SaveProjectReview(_ context.Context, record project.Review) error {
	if err := record.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := filepath.Join(r.root, "project_reviews")
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	return r.writer.Replace(filepath.Join(dir, string(record.RepositoryID)+".json"), record).Err
}

func (r *Repository) LoadProjectReview(_ context.Context, id workspace.RepositoryID) (project.Review, bool, error) {
	if _, err := workspace.ParseRepositoryID(string(id)); err != nil {
		return project.Review{}, false, err
	}
	var record project.Review
	err := readStrict(filepath.Join(r.root, "project_reviews", string(id)+".json"), &record)
	if errors.Is(err, ErrNotFound) {
		return project.Review{}, false, nil
	}
	if err != nil {
		return project.Review{}, false, err
	}
	if err := record.Validate(); err != nil {
		return project.Review{}, false, err
	}
	return record, true, nil
}
