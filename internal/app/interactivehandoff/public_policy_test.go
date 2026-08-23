package interactivehandoff

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func TestH2UnsupportedPrivacyAndAutomaticCompletionFailBeforeAnyStoreOrProviderTouch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*handoff.Request)
	}{
		{"secret", func(req *handoff.Request) { req.Privacy = handoff.PrivacySecret }},
		{"automatic", func(req *handoff.Request) {
			req.Completion = handoff.Completion{Kind: handoff.CompletionEnvironmentExportedNonempty, Name: "TOKEN"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, runtime, _, svc, calls, req := fixture(t)
			tc.mutate(&req)
			*calls = nil
			if _, err := svc.Request(t.Context(), req); !errors.Is(err, failure.FeatureUnavailable) {
				t.Fatalf("unsupported H2 request err=%v", err)
			}
			if len(*calls) != 0 || runtime.attachCalls.Load() != 0 || runtime.signals != 0 {
				t.Fatalf("unsupported H2 request touched store/provider: calls=%v attach=%d signals=%d", *calls, runtime.attachCalls.Load(), runtime.signals)
			}
		})
	}
}
