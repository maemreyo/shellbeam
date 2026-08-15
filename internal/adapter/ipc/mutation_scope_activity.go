package ipc

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

func (s *Server) inspectActivityV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(ActivityActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": req.Action}, nil)
	}
	record, err := actions.InspectActivity(ctx, req.ActivityID)
	resp.Activity = &record
	if err != nil {
		return err
	}
	scoped, ok := s.actions.(ActivityMutationScopeActions)
	if !ok {
		return nil
	}
	result, err := scoped.InspectActivityMutationScopes(ctx, req.ActivityID)
	if err != nil {
		return err
	}
	resp.ActiveMutationScopes = append([]core.Scope(nil), result.ActiveScopes...)
	resp.MutationScopeAdvisories = append([]core.Advisory(nil), result.Advisories...)
	resp.MutationScopesTruncated = result.ScopesTruncated
	resp.MutationScopeAdvisoriesTruncated = result.AdvisoriesTruncated
	return nil
}
