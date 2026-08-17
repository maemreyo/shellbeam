package failure

import (
	"reflect"
	"testing"
)

func TestE27InputTraceFailuresAreStableAndPrivacyFiltered(t *testing.T) {
	candidates := map[string]string{"provider": "dyld-interpose", "platform": "darwin", "reason": "safe_reason", "limit": "32768", "limit_ms": "2000", "trace_id": "trace_01K00000000000000000000000", "operation_id": "op", "private_path": "/Users/alice/secret", "raw_path": "/tmp/raw", "payload": "secret", "environment_value": "token"}
	cases := []struct {
		code      Code
		keys      []string
		retryable bool
	}{
		{InputTraceProviderUnavailable, []string{"provider", "platform", "reason"}, true},
		{InputTraceRequiredUnavailable, []string{"provider", "platform", "reason"}, false},
		{InputTraceStartupBudgetExceeded, []string{"provider", "limit_ms", "reason"}, true},
		{InputTraceUnsupported, []string{"provider", "platform", "reason"}, false},
		{InputTracePartial, []string{"trace_id", "reason"}, false},
		{InputTraceBudgetExceeded, []string{"trace_id", "limit", "reason"}, false},
		{InputTraceLateAttach, []string{"trace_id", "reason"}, false},
		{InputTraceOwnershipLost, []string{"trace_id", "reason"}, false},
		{InputTraceNotFound, []string{"operation_id", "trace_id"}, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			got := New(tc.code, candidates, nil)
			if got.Code != tc.code || got.Retryable != tc.retryable {
				t.Fatalf("failure=%#v", got)
			}
			want := map[string]string{}
			for _, k := range tc.keys {
				want[k] = candidates[k]
			}
			if !reflect.DeepEqual(got.Details, want) {
				t.Fatalf("details=%#v want=%#v", got.Details, want)
			}
		})
	}
}
