package main

import (
	"context"
	"errors"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type resourceCompositionOwner struct{ starts int }

func (o *resourceCompositionOwner) Start(context.Context, operation.ExecutionSpec, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	o.starts++
	return nil, receipt.SpawnEvidence{}, errors.New("test owner should not spawn")
}

func qualifiedResourceSupport() *capability.ResourceEnforcementSupport {
	return &capability.ResourceEnforcementSupport{
		Version: 1, Maturity: "experimental", Provider: "linux_cgroup_v2", Scope: "owned_process_tree", Placement: "pre_exec_atomic",
		MemoryBytes: capability.EnforcementHard, Processes: capability.EnforcementHard,
		CPUTimeMS: capability.EnforcementUnsupported, PersistentSessions: capability.EnforcementUnsupported,
	}
}

func TestResourceCompositionAdvertisesOnlyTheOwnerItWillExecuteWith(t *testing.T) {
	base := capability.Baseline(capability.Limits{}).WithExecutionTelemetry(128, 1<<20, 128, 16, 8, 3600000, 16)
	beforeObservation := *base.ResourceObservation
	owner := &resourceCompositionOwner{}
	calls := 0
	gotOwner, gotCatalog := composeResourceEnforcement(base, func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
		calls++
		return owner, qualifiedResourceSupport(), nil
	})
	if calls != 1 {
		t.Fatalf("factory calls=%d", calls)
	}
	if gotOwner != owner {
		t.Fatalf("composition replaced qualified owner: %T", gotOwner)
	}
	if gotCatalog.Features[capability.FeatureResourceEnforcement] != capability.Available || gotCatalog.ResourceEnforcement == nil {
		t.Fatalf("resource enforcement not advertised: %#v", gotCatalog.ResourceEnforcement)
	}
	if gotCatalog.ResourceEnforcement.Provider != "linux_cgroup_v2" {
		t.Fatalf("provider=%#v", gotCatalog.ResourceEnforcement)
	}
	if gotCatalog.ResourceObservation == nil || *gotCatalog.ResourceObservation != beforeObservation {
		t.Fatalf("resource observation changed while enabling enforcement: before=%#v after=%#v", beforeObservation, gotCatalog.ResourceObservation)
	}
}

func TestResourceCompositionUnqualifiedProviderLeavesCapabilityUnavailable(t *testing.T) {
	for name, factory := range map[string]resourceOwnerFactory{
		"qualification_error": func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
			return &resourceCompositionOwner{}, nil, errors.New("no delegated cgroup")
		},
		"missing_support": func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
			return &resourceCompositionOwner{}, nil, nil
		},
		"invalid_support": func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
			bad := qualifiedResourceSupport()
			bad.Processes = capability.EnforcementUnsupported
			return &resourceCompositionOwner{}, bad, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			base := capability.Baseline(capability.Limits{})
			owner, catalog := composeResourceEnforcement(base, factory)
			if owner == nil {
				t.Fatal("unqualified composition returned nil ordinary process owner")
			}
			if catalog.Features[capability.FeatureResourceEnforcement] == capability.Available || catalog.ResourceEnforcement != nil {
				t.Fatalf("unqualified provider advertised: %#v", catalog.ResourceEnforcement)
			}
		})
	}
}

func TestResourceCompositionDefaultWithoutConfigurationStaysUnavailable(t *testing.T) {
	t.Setenv("SHELLBEAM_RESOURCE_CGROUP_ROOT", "")
	owner, catalog := composeResourceEnforcement(capability.Baseline(capability.Limits{}), nil)
	if owner == nil {
		t.Fatal("default composition returned nil process owner")
	}
	if catalog.ResourceEnforcement != nil || catalog.Features[capability.FeatureResourceEnforcement] == capability.Available {
		t.Fatalf("unconfigured daemon advertised resource enforcement: %#v", catalog.ResourceEnforcement)
	}
}

func TestResourceCompositionDoesNotProbeWhenFactoryReturnsNoSupport(t *testing.T) {
	owner, catalog := composeResourceEnforcement(capability.Baseline(capability.Limits{}), func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
		return &resourceCompositionOwner{}, nil, nil
	})
	if owner == nil || catalog.ResourceEnforcement != nil {
		t.Fatalf("owner=%T catalog=%#v", owner, catalog.ResourceEnforcement)
	}
}
