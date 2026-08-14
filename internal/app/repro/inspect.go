package repro

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type ReferenceResolution struct {
	RefID           string               `json:"ref_id"`
	RecordKind      string               `json:"record_kind"`
	ResolutionState core.ResolutionState `json:"resolution_state"`
}

type InspectResult struct {
	SchemaVersion int                   `json:"schema_version"`
	Capsule       core.Capsule          `json:"capsule"`
	References    []ReferenceResolution `json:"references"`
}

func (s *Service) Inspect(ctx context.Context, reproID string) (InspectResult, error) {
	if s == nil || s.repository == nil {
		return InspectResult{}, failure.New(failure.ReproMaterializationUnavailable, nil, nil)
	}
	capsule, found, err := s.repository.GetRepro(ctx, reproID)
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return InspectResult{}, failure.New(failure.ReproNotFound, map[string]string{"repro_id": reproID}, nil)
	}
	result := InspectResult{SchemaVersion: 1, Capsule: capsule, References: make([]ReferenceResolution, 0, len(capsule.Results))}
	for _, descriptor := range capsule.Results {
		state, err := s.resolveReference(ctx, capsule, descriptor)
		if err != nil {
			return InspectResult{}, err
		}
		result.References = append(result.References, ReferenceResolution{RefID: descriptor.RefID, RecordKind: descriptor.RecordKind, ResolutionState: state})
	}
	return result, nil
}

func (s *Service) resolveReference(ctx context.Context, capsule core.Capsule, descriptor core.ReferenceDescriptor) (core.ResolutionState, error) {
	if descriptor.OriginalAvailability == core.AvailabilityAbsent || descriptor.Digest == "" {
		return core.ResolutionUnavailable, nil
	}
	switch descriptor.RecordKind {
	case "structured_result":
		current, found, err := s.repository.FindOperationDerivation(ctx, capsule.Execution.OperationID)
		if err != nil {
			return core.ResolutionUnavailable, err
		}
		if !found {
			if descriptor.OriginalAvailability == core.AvailabilityTerminal {
				return core.ResolutionPurged, nil
			}
			return core.ResolutionUnavailable, nil
		}
		if current.DerivationKey != descriptor.Digest {
			return core.ResolutionUnavailable, nil
		}
		if current.Completeness == structured.CompletenessCompacted {
			return core.ResolutionCompacted, nil
		}
		return core.ResolutionAvailable, nil
	case "execution_telemetry":
		current, found, err := s.repository.FindPerformanceByOperation(ctx, capsule.Execution.OperationID)
		if err != nil {
			return core.ResolutionUnavailable, err
		}
		if !found {
			return core.ResolutionPurged, nil
		}
		if current.DerivationKey != descriptor.Digest {
			return core.ResolutionUnavailable, nil
		}
		return core.ResolutionAvailable, nil
	default:
		return core.ResolutionUnavailable, nil
	}
}
