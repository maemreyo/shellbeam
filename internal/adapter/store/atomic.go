package store

import (
	"encoding/json"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"os"
	"path/filepath"
)

func atomicJSON(path string, v any) app.StoreResult {
	b, err := json.Marshal(v)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".shellbeam-*")
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	name := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	cerr := tmp.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err = os.Rename(name, path); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	renamed = true
	d, err := os.Open(dir)
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	err = d.Sync()
	_ = d.Close()
	if err != nil {
		return app.StoreResult{Durability: app.AmbiguousChange, Err: err}
	}
	return app.StoreResult{Durability: app.DurableChange}
}
