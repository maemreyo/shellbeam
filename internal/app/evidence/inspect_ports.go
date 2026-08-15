package evidence

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type InspectionRepository interface {
	ObservationHighWatermark(context.Context) (observation.ChangeSeq, error)
	ListEvidenceIndexObligations(context.Context, observation.ChangeSeq, observation.ChangeSeq, int) ([]observation.ObservationObligation, error)
	FindEvidenceByID(context.Context, string) (core.Record, bool, error)
	FindEvidenceByOperation(context.Context, operation.ID) (core.Record, bool, error)
	LoadEvidenceValidity(context.Context, string) (core.ValidityObservation, bool, error)
	PutEvidenceValidity(context.Context, core.ValidityObservation) (bool, error)
}

type CurrentState struct {
	Source        core.CurrentSource `json:"source"`
	WorkspaceRoot string             `json:"-"`
}

type CurrentStateProvider interface {
	ObserveCurrent(context.Context, core.Record) CurrentState
}
