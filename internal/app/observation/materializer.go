package observation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

const materializeBatchSize = 256

type MaterializeResult struct {
	State         core.ProjectionState
	HighWatermark core.ChangeSeq
	PreparedGapAt core.ChangeSeq
}

type Materializer struct {
	store JournalStore
	mu    sync.Mutex
}

func NewMaterializer(store JournalStore) *Materializer {
	return &Materializer{store: store}
}

func (m *Materializer) Materialize(ctx context.Context) (MaterializeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.LoadEventProjectionState(ctx)
	if err != nil {
		return MaterializeResult{}, err
	}
	key, err := m.store.EventCursorKey(ctx)
	if err != nil {
		return MaterializeResult{}, err
	}
	high, err := m.store.ObservationHighWatermark(ctx)
	if err != nil {
		return MaterializeResult{}, err
	}
	if state.MaterializedThroughSeq > high {
		return MaterializeResult{}, fmt.Errorf("event projection exceeds observation high watermark")
	}
	for state.MaterializedThroughSeq < high {
		batch, err := m.store.ListObservationObligations(ctx, state.MaterializedThroughSeq, materializeBatchSize)
		if err != nil {
			return MaterializeResult{}, err
		}
		if len(batch) == 0 {
			return MaterializeResult{}, fmt.Errorf("observation obligation continuity gap")
		}
		progressed := false
		for _, obligation := range batch {
			if obligation.ChangeSeq > high {
				break
			}
			expected := state.MaterializedThroughSeq + 1
			if obligation.ChangeSeq != expected {
				return MaterializeResult{}, fmt.Errorf("observation obligation sequence gap")
			}
			if obligation.State == core.ObligationPrepared {
				return MaterializeResult{State: state, HighWatermark: high, PreparedGapAt: obligation.ChangeSeq}, nil
			}
			if obligation.State == core.ObligationCommitted {
				event := eventFromObligation(key.StateRootEpoch, obligation)
				if err := m.store.PutEvent(ctx, event); err != nil {
					return MaterializeResult{State: state, HighWatermark: high}, err
				}
			}
			state.MaterializedThroughSeq = obligation.ChangeSeq
			if err := m.store.SaveEventProjectionState(ctx, state); err != nil {
				return MaterializeResult{State: state, HighWatermark: high}, err
			}
			progressed = true
		}
		if !progressed {
			break
		}
		latest, err := m.store.ObservationHighWatermark(ctx)
		if err != nil {
			return MaterializeResult{}, err
		}
		high = latest
	}
	return MaterializeResult{State: state, HighWatermark: high}, nil
}

func eventFromObligation(epoch string, obligation core.ObservationObligation) core.Event {
	identity := fmt.Sprintf("%s\x00%d\x00%s\x00%s", epoch, obligation.ChangeSeq, obligation.Kind, obligation.SubjectRef)
	sum := sha256.Sum256([]byte(identity))
	return core.Event{
		SchemaVersion:  core.SchemaVersion,
		EventID:        "evt_" + hex.EncodeToString(sum[:]),
		StateRootEpoch: epoch,
		ChangeSeq:      obligation.ChangeSeq,
		Kind:           obligation.Kind,
		RecordedAt:     obligation.PreparedAt.UTC(),
		Correlation:    obligation.Correlation,
		SubjectRef:     obligation.SubjectRef,
		Summary:        obligation.Summary,
	}
}
