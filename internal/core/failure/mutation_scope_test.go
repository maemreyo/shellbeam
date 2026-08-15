package failure

import "testing"

func TestA26MutationScopeFailuresExposeOnlySafeDetails(t *testing.T) {
	tests := []struct {
		code        Code
		wantMessage string
		retryable   bool
	}{
		{MutationScopeInvalid, "mutation scope is invalid", false},
		{MutationScopeBindingConflict, "mutation scope binding conflicts with existing binding", false},
		{MutationMetadataConflict, "mutation metadata conflicts with existing mutation", false},
		{MutationScopeCapacityExceeded, "mutation scope capacity exceeded", true},
		{PersistenceAmbiguous, "persistence result is ambiguous", true},
	}
	for _, tc := range tests {
		got := New(tc.code, map[string]string{"scope_id": "safe-scope", "field": "paths", "reason": "invalid_selector", "path": "/private/secret", "command": "rm -rf /"}, nil)
		if got.Message != tc.wantMessage || got.Retryable != tc.retryable {
			t.Fatalf("code=%s got=%#v", tc.code, got)
		}
		if _, ok := got.Details["path"]; ok {
			t.Fatalf("code=%s leaked path", tc.code)
		}
		if _, ok := got.Details["command"]; ok {
			t.Fatalf("code=%s leaked command", tc.code)
		}
	}
}
