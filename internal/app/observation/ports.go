// Package observationapp owns bounded execution-observation use cases.
package observation

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

type JournalStore interface {
	ObservationHighWatermark(context.Context) (core.ChangeSeq, error)
	ListObservationObligations(context.Context, core.ChangeSeq, int) ([]core.ObservationObligation, error)
	EventCursorKey(context.Context) (core.CursorKeyMaterial, error)
	PutEvent(context.Context, core.Event) error
	LoadEventProjectionState(context.Context) (core.ProjectionState, error)
	SaveEventProjectionState(context.Context, core.ProjectionState) error
	ListEvents(context.Context, core.ChangeSeq, core.ChangeSeq, int) ([]core.Event, bool, error)
}

type SnapshotProvider interface {
	CaptureSnapshot(context.Context, core.Target) (core.Snapshot, error)
}

type MaterializerPort interface {
	Materialize(context.Context) (MaterializeResult, error)
}
