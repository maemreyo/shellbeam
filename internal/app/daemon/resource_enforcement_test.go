package daemon

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func hardResourceCatalog() capability.Catalog {
	return capability.Baseline(capability.Limits{}).WithResourceEnforcement(capability.ResourceEnforcementSupport{
		Version: 1, Maturity: "experimental", Provider: "linux_cgroup_v2", Scope: "owned_process_tree", Placement: "pre_exec_atomic",
		MemoryBytes: capability.EnforcementHard, Processes: capability.EnforcementHard,
		CPUTimeMS: capability.EnforcementUnsupported, PersistentSessions: capability.EnforcementUnsupported,
	})
}

func TestValidateResourceLimitsFailsClosedByCapabilityAndContract(t *testing.T) {
	memory := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	cases := []struct {
		name    string
		catalog capability.Catalog
		req     StartRequest
		code    failure.Code
		metric  string
		reason  string
	}{
		{name: "legacy protocol", catalog: hardResourceCatalog(), req: StartRequest{ProtocolVersion: 1, ResourceLimits: memory}, code: failure.ResourceLimitUnsupported, metric: "resource_limits", reason: "protocol_v2_required"},
		{name: "provider unavailable", catalog: capability.Baseline(capability.Limits{}), req: StartRequest{ProtocolVersion: 2, ResourceLimits: memory}, code: failure.ResourceLimitUnsupported, metric: "resource_limits", reason: "provider_unavailable"},
		{name: "persistent", catalog: hardResourceCatalog(), req: StartRequest{ProtocolVersion: 2, Persistent: true, ResourceLimits: memory}, code: failure.ResourceLimitUnsupported, metric: "resource_limits", reason: "persistent"},
		{name: "cpu", catalog: hardResourceCatalog(), req: StartRequest{ProtocolVersion: 2, ResourceLimits: &operation.ResourceLimits{CPUTimeMS: 1000}}, code: failure.ResourceLimitUnsupported, metric: "cpu_time_ms", reason: "hard_unsupported"},
		{name: "empty", catalog: hardResourceCatalog(), req: StartRequest{ProtocolVersion: 2, ResourceLimits: &operation.ResourceLimits{}}, code: failure.InvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResourceLimits(tc.catalog, tc.req)
			var typed *failure.Failure
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("err=%v typed=%#v want code=%s", err, typed, tc.code)
			}
			if tc.metric != "" && typed.Details["metric"] != tc.metric {
				t.Fatalf("metric=%q want=%q details=%#v", typed.Details["metric"], tc.metric, typed.Details)
			}
			if tc.reason != "" && typed.Details["reason"] != tc.reason {
				t.Fatalf("reason=%q want=%q details=%#v", typed.Details["reason"], tc.reason, typed.Details)
			}
		})
	}
	if err := validateResourceLimits(hardResourceCatalog(), StartRequest{ProtocolVersion: 2, ResourceLimits: &operation.ResourceLimits{MemoryBytes: 64 << 20, Processes: 8}}); err != nil {
		t.Fatalf("valid hard limits rejected: %v", err)
	}
	if err := validateResourceLimits(capability.Baseline(capability.Limits{}), StartRequest{}); err != nil {
		t.Fatalf("omitted limits should be a no-op: %v", err)
	}
}
