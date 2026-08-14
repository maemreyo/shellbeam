package store

import (
	"context"
	"errors"
	"path/filepath"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
)

func (r *Repository) SaveActivity(_ context.Context, record activity.Activity) error {
	if err := record.Validate(0); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := filepath.Join(r.root, "activities", string(record.ID))
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	return r.writer.Replace(filepath.Join(dir, "index.json"), record).Err
}

func (r *Repository) LoadActivity(_ context.Context, id activity.ID) (activity.Activity, bool, error) {
	if _, err := activity.ParseID(string(id)); err != nil {
		return activity.Activity{}, false, err
	}
	var record activity.Activity
	err := readStrict(filepath.Join(r.root, "activities", string(id), "index.json"), &record)
	if errors.Is(err, ErrNotFound) {
		return activity.Activity{}, false, nil
	}
	if err != nil {
		return activity.Activity{}, false, err
	}
	if err := record.Validate(0); err != nil {
		return activity.Activity{}, false, err
	}
	return record, true, nil
}
