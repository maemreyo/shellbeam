package daemon

import (
	"context"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (s *Service) lookupV2Replay(ctx context.Context, req StartRequest, id operation.ID, intent operation.Intent) (View, bool, error) {
	if req.ProtocolVersion != 2 {
		return View{}, false, nil
	}
	requestFingerprint, err := intent.RequestFingerprint()
	if err != nil {
		return View{}, true, invalidIntentFailure(err)
	}
	stored, found, err := s.store.FindOperation(ctx, id)
	if err != nil {
		return View{}, true, failure.Normalize(err)
	}
	if !found {
		return View{}, false, nil
	}
	if stored.EffectiveRequestFingerprint() != requestFingerprint {
		return View{}, true, failure.New(failure.OperationConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID}).Fingerprint()
	if err != nil {
		return View{}, true, invalidIntentFailure(err)
	}
	if stored.ObservationBindingFingerprint != observationFingerprint {
		return View{}, true, failure.New(failure.OperationMetadataConflict, map[string]string{"operation_id": string(id)}, nil)
	}
	view, err := s.waitView(ctx, stored, string(stored.SessionID), 0, req.YieldMS, req.MaxOutputBytes)
	if err == nil {
		s.enrichReplayWorkspaceContext(ctx, &view, stored.CWD, req.WorkspaceHint)
	}
	return view, true, err
}

func (s *Service) resolveStartIntent(ctx context.Context, req StartRequest) (operation.Intent, error) {
	intent := operation.Intent{Command: req.Command, WorkspaceID: req.WorkspaceID, CWD: req.CWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS}
	if req.ProtocolVersion != 2 || req.WorkspaceID == "" {
		intent.ResolvedCWD = req.CWD
		return intent, nil
	}
	address := workspace.Address{WorkspaceID: workspace.WorkspaceID(req.WorkspaceID), CWD: req.CWD}
	if err := address.Validate(); err != nil {
		return operation.Intent{}, invalidIntentFailure(err)
	}
	if s.resolver == nil {
		return operation.Intent{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "workspace_addressing"}, nil)
	}
	resolved, err := s.resolver.ResolveAddress(ctx, address)
	if err != nil {
		return operation.Intent{}, failure.Normalize(err)
	}
	if resolved.WorkspaceID != address.WorkspaceID || resolved.CWD == "" {
		return operation.Intent{}, failure.New(failure.Internal, nil, fmt.Errorf("workspace resolver identity mismatch"))
	}
	intent.CWD = resolved.LogicalCWD
	intent.ResolvedCWD = resolved.CWD
	return intent, nil
}
