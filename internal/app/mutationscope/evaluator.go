package mutationscope

import (
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	"sort"
	"time"
)

func activeScopesAt(scopes []core.Scope, now time.Time) []core.Scope {
	out := make([]core.Scope, 0, len(scopes))
	for _, item := range scopes {
		if item.Validate() == nil && now.Before(item.ExpiresAt) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScopeID < out[j].ScopeID })
	return out
}

func selectedScopes(scopes []core.Scope, activityID string, limit int) ([]core.Scope, int, bool) {
	selected := make([]core.Scope, 0, len(scopes))
	for _, item := range scopes {
		if activityID == "" || item.ActivityID == activityID {
			selected = append(selected, item)
		}
	}
	total := len(selected)
	if limit >= 0 && len(selected) > limit {
		return append([]core.Scope(nil), selected[:limit]...), total, true
	}
	return selected, total, false
}

func evaluateAdvisories(scopes []core.Scope, activityID, focusScopeID string, limit int) ([]core.Advisory, int, bool) {
	out := make([]core.Advisory, 0)
	for i := 0; i < len(scopes); i++ {
		for j := i + 1; j < len(scopes); j++ {
			left, right := scopes[i], scopes[j]
			if focusScopeID != "" && left.ScopeID != focusScopeID && right.ScopeID != focusScopeID {
				continue
			}
			if activityID != "" && left.ActivityID != activityID && right.ActivityID != activityID {
				continue
			}
			if advisory, ok := core.BuildAdvisory(left, right, core.MaxOverlapExamples); ok {
				out = append(out, advisory)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WorkspaceID != out[j].WorkspaceID {
			return out[i].WorkspaceID < out[j].WorkspaceID
		}
		if out[i].ScopeIDs[0] != out[j].ScopeIDs[0] {
			return out[i].ScopeIDs[0] < out[j].ScopeIDs[0]
		}
		return out[i].ScopeIDs[1] < out[j].ScopeIDs[1]
	})
	total := len(out)
	if limit >= 0 && len(out) > limit {
		return append([]core.Advisory(nil), out[:limit]...), total, true
	}
	return out, total, false
}
