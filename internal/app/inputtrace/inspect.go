package inputtrace

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type InspectStatus string

const (
	InspectUnavailable InspectStatus = "unavailable"
	InspectPending     InspectStatus = "pending"
	InspectTerminal    InspectStatus = "terminal"
)

type InspectRequest struct {
	OperationID  string `json:"operation_id"`
	MaxResources int    `json:"max_resources"`
}
type InspectResult struct {
	SchemaVersion      int           `json:"schema_version"`
	Status             InspectStatus `json:"status"`
	OperationID        string        `json:"operation_id"`
	TraceID            string        `json:"trace_id,omitempty"`
	Record             *core.Record  `json:"record,omitempty"`
	ResourcesReturned  int           `json:"resources_returned,omitempty"`
	ResourcesAvailable int           `json:"resources_available,omitempty"`
}

type InspectionRepository interface {
	FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error)
	LoadInputTraceByOperation(context.Context, string) (core.Record, bool, error)
}
type Inspector struct{ repository InspectionRepository }

func NewInspector(repository InspectionRepository) *Inspector {
	return &Inspector{repository: repository}
}

func (s *Inspector) Inspect(ctx context.Context, request InspectRequest) (InspectResult, error) {
	if s == nil || s.repository == nil {
		return InspectResult{}, fmt.Errorf("input trace inspection unavailable")
	}
	id, err := operation.ParseID(request.OperationID)
	if err != nil {
		return InspectResult{}, failure.New(failure.InvalidInput, map[string]string{"field": "operation_id"}, err)
	}
	if request.MaxResources < 1 || request.MaxResources > core.MaxPublicResources {
		return InspectResult{}, failure.New(failure.InvalidInput, map[string]string{"field": "max_resources"}, nil)
	}
	reservation, found, err := s.repository.FindOperation(ctx, id)
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return InspectResult{}, failure.New(failure.InputTraceNotFound, map[string]string{"operation_id": request.OperationID}, nil)
	}
	base := InspectResult{SchemaVersion: core.SchemaVersion, Status: InspectUnavailable, OperationID: request.OperationID}
	if reservation.Trace == nil {
		return base, nil
	}
	base.TraceID = reservation.Trace.TraceID
	base.Status = InspectPending
	record, found, err := s.repository.LoadInputTraceByOperation(ctx, request.OperationID)
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return base, nil
	}
	if err := record.Validate(); err != nil {
		return InspectResult{}, err
	}
	copy := record
	copy.Resources = append([]core.Resource(nil), record.Resources...)
	available := len(copy.Resources)
	if len(copy.Resources) > request.MaxResources {
		copy.Resources = copy.Resources[:request.MaxResources]
	}
	copy.Summary.ResourcesReturned = len(copy.Resources)
	base.Status = InspectTerminal
	base.Record = &copy
	base.ResourcesAvailable = available
	base.ResourcesReturned = len(copy.Resources)
	return base, nil
}
