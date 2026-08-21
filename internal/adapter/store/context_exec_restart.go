package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) ListContextExecRecoveryCandidates(ctx context.Context) ([]operation.ContextExecState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.contextExecDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]operation.ContextExecState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validContextExecStoreID(id) || filepath.Base(entry.Name()) != entry.Name() {
			return nil, errors.New("unsafe context exec recovery entry")
		}
		record, err := r.loadContextExecRecordUnlocked(id)
		if err != nil {
			return nil, err
		}
		if !record.State.Lifecycle.Terminal() {
			out = append(out, record.State.Clone())
		}
	}
	return out, nil
}
