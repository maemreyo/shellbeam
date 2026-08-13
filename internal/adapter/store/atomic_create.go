package store

import (
	"errors"
	"os"
	"path/filepath"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
)

func (w atomicWriter) Create(path string, v any) app.StoreResult {
	name, result := w.tempJSON("create", path, v)
	if result.Err != nil {
		return result
	}
	defer os.Remove(name)
	if err := w.checkpoint("create.link"); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := os.Link(name, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return app.StoreResult{Durability: app.NoDurableChange, Err: os.ErrExist}
		}
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	return w.syncParent("create", filepath.Dir(path))
}

func atomicCreateJSON(path string, v any) app.StoreResult {
	return (atomicWriter{}).Create(path, v)
}
