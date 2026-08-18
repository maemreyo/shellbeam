package daemon

import (
	"context"
	"fmt"

	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type preparedHermetic struct {
	execution hermeticapp.PreparedExecution
	binding   *hermeticcore.BoundaryBinding
}

func (s *Service) prepareHermeticExecution(ctx context.Context, req StartRequest, logicalCWD string, spec operation.ExecutionSpec) (*preparedHermetic, error) {
	if req.Hermetic == nil {
		return nil, nil
	}
	if s.options.HermeticRuntime == nil {
		return nil, fmt.Errorf("hermetic runtime unavailable after admission")
	}
	canonical, err := req.Hermetic.Canonical()
	if err != nil {
		return nil, err
	}
	// Hermetic V1 closes stdin by contract even when the ordinary policy field
	// was omitted. Keep that fact in the private target spec and terminal receipt.
	spec.StdinMode = operation.StdinModeClosed
	prepared, err := s.options.HermeticRuntime.Prepare(ctx, HermeticPrepareRequest{WorkspaceID: req.WorkspaceID, LogicalCWD: logicalCWD, Request: canonical, Target: spec})
	if err != nil {
		return nil, err
	}
	if err := prepared.ValidatePrivate(); err != nil {
		_ = s.options.HermeticRuntime.Discard(context.Background(), prepared)
		return nil, err
	}
	binding := &hermeticcore.BoundaryBinding{
		SchemaVersion:         hermeticcore.BoundaryBindingSchemaV1,
		BoundaryID:            prepared.BoundaryID,
		Request:               canonical,
		CaptureManifestSHA256: prepared.CaptureManifestSHA256,
		Provider:              prepared.Provider,
		Toolchain:             prepared.Toolchain,
	}
	if err := binding.Validate(); err != nil {
		_ = s.options.HermeticRuntime.Discard(context.Background(), prepared)
		return nil, err
	}
	return &preparedHermetic{execution: prepared, binding: binding}, nil
}

func (s *Service) discardHermetic(prepared *preparedHermetic) {
	if prepared == nil || s.options.HermeticRuntime == nil {
		return
	}
	_ = s.options.HermeticRuntime.Discard(context.Background(), prepared.execution)
}

func lostHermeticResult(binding *hermeticcore.BoundaryBinding) *hermeticcore.BoundaryResult {
	if binding == nil {
		return nil
	}
	return &hermeticcore.BoundaryResult{
		SchemaVersion:      hermeticcore.BoundaryResultSchemaV1,
		BoundaryID:         binding.BoundaryID,
		Provider:           binding.Provider,
		Toolchain:          binding.Toolchain,
		EstablishedPreExec: false,
		Continuity:         hermeticcore.ContinuityLost,
	}
}
