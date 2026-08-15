package mutationscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func normalizeSetRequest(request SetRequest) (SetRequest, string, error) {
	if err := core.ValidateMutationID(request.MutationID); err != nil {
		return request, "", invalidScope("mutation_id", "invalid_id", err)
	}
	if err := core.ValidateScopeID(request.ScopeID); err != nil {
		return request, "", invalidScope("scope_id", "invalid_id", err)
	}
	if _, err := activity.ParseID(request.ActivityID); err != nil {
		return request, "", invalidScope("activity_id", "invalid_id", err)
	}
	if _, err := workspace.ParseWorkspaceID(string(request.WorkspaceID)); err != nil {
		return request, "", invalidScope("workspace_id", "invalid_id", err)
	}
	if request.Mode != core.ModeRead && request.Mode != core.ModeMutate {
		return request, "", invalidScope("mode", "invalid_mode", nil)
	}
	paths, err := core.NormalizeSelectors(request.Paths)
	if err != nil {
		return request, "", invalidScope("paths", "invalid_selector", err)
	}
	request.Paths = paths
	if request.TTLMS == 0 {
		request.TTLMS = core.DefaultTTL.Milliseconds()
	}
	if request.TTLMS < core.MinTTL.Milliseconds() || request.TTLMS > core.MaxTTL.Milliseconds() {
		return request, "", invalidScope("ttl_ms", "out_of_range", nil)
	}
	fingerprint, err := setRequestFingerprint(request)
	if err != nil {
		return request, "", err
	}
	return request, fingerprint, nil
}

func normalizeReleaseRequest(request ReleaseRequest) (ReleaseRequest, string, error) {
	if err := core.ValidateMutationID(request.MutationID); err != nil {
		return request, "", invalidScope("mutation_id", "invalid_id", err)
	}
	if err := core.ValidateScopeID(request.ScopeID); err != nil {
		return request, "", invalidScope("scope_id", "invalid_id", err)
	}
	fingerprint, err := digestCanonical(struct {
		SchemaVersion int    `json:"schema_version"`
		Action        string `json:"action"`
		ScopeID       string `json:"scope_id"`
	}{core.SchemaVersion, "mutation_scope.release", request.ScopeID})
	return request, fingerprint, err
}

func setRequestFingerprint(request SetRequest) (string, error) {
	return digestCanonical(struct {
		SchemaVersion int                   `json:"schema_version"`
		Action        string                `json:"action"`
		ScopeID       string                `json:"scope_id"`
		ActivityID    string                `json:"activity_id"`
		WorkspaceID   workspace.WorkspaceID `json:"workspace_id"`
		Mode          core.Mode             `json:"mode"`
		Paths         []string              `json:"paths"`
		TTLMS         int64                 `json:"ttl_ms"`
	}{core.SchemaVersion, "mutation_scope.set", request.ScopeID, request.ActivityID, request.WorkspaceID, request.Mode, request.Paths, request.TTLMS})
}

func digestCanonical(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode mutation request: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func invalidScope(field, reason string, cause error) error {
	return failure.New(failure.MutationScopeInvalid, map[string]string{"field": field, "reason": reason}, cause)
}
func ttlDuration(request SetRequest) time.Duration {
	return time.Duration(request.TTLMS) * time.Millisecond
}
