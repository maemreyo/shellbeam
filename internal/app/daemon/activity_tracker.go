package daemon

import (
	"context"
	"time"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type ActivityTracker interface {
	Admit(context.Context, activity.Admission) (activity.Activity, error)
}

func NewServiceWithActivityTracker(store Store, owner ProcessOwner, tracker ActivityTracker, options Options) *Service {
	service := NewService(store, owner, options)
	service.activityTracker = tracker
	return service
}

func (s *Service) validateActivityRequest(req StartRequest) error {
	if req.ActivityID == "" {
		return nil
	}
	if req.ProtocolVersion != 2 || s.activityTracker == nil {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "activities", "required_version": "2"}, nil)
	}
	if _, err := activity.ParseID(req.ActivityID); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "activity_id"}, err)
	}
	return nil
}

func (s *Service) admitActivity(ctx context.Context, req StartRequest, sessionID string, observation workspaceObservation) string {
	if req.ActivityID == "" || s.activityTracker == nil {
		return ""
	}
	var workspaceID workspace.WorkspaceID
	if observation.pre != nil {
		workspaceID = observation.pre.WorkspaceID
	}
	_, _ = s.activityTracker.Admit(ctx, activity.Admission{
		ActivityID: activity.ID(req.ActivityID), OperationID: req.OperationID, SessionID: sessionID,
		WorkspaceID: workspaceID, CWD: req.CWD, ObservedAt: time.Now().UTC(),
	})
	return req.ActivityID
}
