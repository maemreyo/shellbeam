package failure

import (
	"reflect"
	"testing"
)

func TestE26CheckpointFailuresAreStableAndPrivacyFiltered(t *testing.T) {
	candidates := map[string]string{
		"provider": "localfs", "reason": "safe_reason", "checkpoint_create_id": "cp-create-1",
		"checkpoint_id": "chk_01K00000000000000000000000", "restore_id": "restore-1",
		"workspace_id": "ws_01K00000000000000000000000", "field": "paths",
		"path": "internal/runtime/file.go", "limit": "64",
		"private_path": "/Users/example/.secret", "content_hash": "dictionary-comparable", "raw_content": "secret",
	}
	cases := []struct {
		code Code
		keys []string
	}{
		{CheckpointProviderUnavailable, []string{"provider", "reason"}},
		{CheckpointCreateConflict, []string{"checkpoint_create_id"}},
		{CheckpointScopeInvalid, []string{"field", "reason"}},
		{CheckpointScopeTooLarge, []string{"field", "reason", "limit"}},
		{CheckpointPathUnsupported, []string{"path", "reason"}},
		{CheckpointSubmoduleBoundaryUnsupported, []string{"path"}},
		{CheckpointBudgetExceeded, []string{"field", "reason", "limit"}},
		{CheckpointNotFound, []string{"checkpoint_id"}},
		{CheckpointExpired, []string{"checkpoint_id"}},
		{CheckpointRestoreRequestConflict, []string{"restore_id"}},
		{CheckpointRestoreConflict, []string{"restore_id", "checkpoint_id", "path"}},
		{CheckpointRestorePartial, []string{"restore_id", "checkpoint_id"}},
		{CheckpointRestoreFailed, []string{"restore_id", "checkpoint_id", "reason"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			got := New(tc.code, candidates, nil)
			if got.Code != tc.code || got.Message == "" {
				t.Fatalf("failure not registered: %#v", got)
			}
			want := make(map[string]string, len(tc.keys))
			for _, key := range tc.keys {
				want[key] = candidates[key]
			}
			if !reflect.DeepEqual(got.Details, want) {
				t.Fatalf("details=%v want=%v", got.Details, want)
			}
			for _, forbidden := range []string{"private_path", "content_hash", "raw_content"} {
				if _, ok := got.Details[forbidden]; ok {
					t.Fatalf("privacy-unsafe detail survived: %s", forbidden)
				}
			}
		})
	}
}
