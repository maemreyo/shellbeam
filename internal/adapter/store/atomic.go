package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
)

type atomicWriter struct {
	fail func(string) error
	// onBytes reports the size of each durable JSON write to the admission
	// index. Replaces report their full size rather than their growth, and
	// deletions are not reported at all, so the running total is an
	// overestimate -- the safe direction for a budget guard, and corrected by
	// the periodic exact re-derive.
	onBytes func(int64)
}

func (w atomicWriter) Replace(path string, v any) app.StoreResult {
	name, result := w.tempJSON("replace", path, v)
	if result.Err != nil {
		return result
	}
	defer os.Remove(name)
	if err := w.checkpoint("replace.rename"); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := os.Rename(name, path); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	return w.syncParent("replace", filepath.Dir(path))
}

func (w atomicWriter) tempJSON(kind, path string, v any) (string, app.StoreResult) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err = w.checkpoint(kind + ".create_temp"); err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".shellbeam-*")
	if err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0600); err == nil {
		err = w.checkpoint(kind + ".write")
	}
	if err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if err == nil {
		err = w.checkpoint(kind + ".file_sync")
	}
	if err == nil {
		err = tmp.Sync()
	}
	if err == nil {
		err = w.checkpoint(kind + ".close")
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	ok = true
	if w.onBytes != nil {
		w.onBytes(int64(len(b) + 1))
	}
	return name, app.StoreResult{Durability: app.DurableChange}
}

func (w atomicWriter) syncParent(kind, dir string) app.StoreResult {
	if err := w.checkpoint(kind + ".open_dir"); err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	d, err := os.Open(dir)
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	defer d.Close()
	if err = w.checkpoint(kind + ".dir_sync"); err == nil {
		err = d.Sync()
	}
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return app.StoreResult{Durability: app.DurableChange}
}

func (w atomicWriter) checkpoint(point string) error {
	if w.fail == nil {
		return nil
	}
	return w.fail(point)
}

func atomicJSON(path string, v any) app.StoreResult {
	return (atomicWriter{}).Replace(path, v)
}
