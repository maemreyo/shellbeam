package main

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestDoctorContextExecReportsQualifiedContractWithoutPrivateMaterial(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{}).
		WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).
		WithInteractiveHandoff(capability.InteractiveHandoffSupport{
			ManualStandard: true, Secret: true,
			Privacy:          &capability.HandoffPrivacySupport{SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true, HumanInputPersisted: false},
			CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
		}).
		WithContextExec(capability.ContextExecSupport{
			ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin",
			ShellAdapters: []shellcore.ShellFamily{shellcore.ShellFish, shellcore.ShellZsh, shellcore.ShellBash}, HelperProtocolVersion: 3,
			EvidenceAuthority:   contextcore.EvidenceAuthorityContextExecChildOwnedV1,
			EvidenceQualities:   []contextcore.EvidenceQuality{contextcore.EvidenceQualityUnproven, contextcore.EvidenceQualityIncomplete, contextcore.EvidenceQualityComplete, contextcore.EvidenceQualityAmbiguous},
			OutputAttribution:   contextcore.OutputAttributionHelperOwnedChildPipes,
			ResourceEnforcement: capability.Unavailable, Hermetic: capability.Unavailable,
		})
	check := doctorContextExecCheck(catalog)
	if check.ID != "context_exec" || check.Status != "pass" || check.Message != "context execution available" {
		t.Fatalf("check=%#v", check)
	}
	for _, required := range []string{
		"provider=tmux_control_mode", "shell_adapters=fish,zsh,bash", "helper_protocol=3",
		"evidence_authority=context_exec_child_owned_v1", "evidence_qualities=unproven,incomplete,complete,ambiguous",
		"resource_enforcement=unavailable", "hermetic=unavailable", "blockers=none",
	} {
		if !strings.Contains(check.Hint, required) {
			t.Fatalf("missing %q in hint %q", required, check.Hint)
		}
	}
	for _, forbidden := range []string{"TOKEN=", "SECRET=", "opaque_launch", "generation=", "request_fingerprint", "cwd=", "environment="} {
		if strings.Contains(check.Hint, forbidden) {
			t.Fatalf("doctor leaked %q: %q", forbidden, check.Hint)
		}
	}
}

func TestDoctorContextExecExplainsCapabilityBlockersWithoutSecrets(t *testing.T) {
	check := doctorContextExecCheck(capability.Baseline(capability.Limits{}))
	if check.ID != "context_exec" || check.Status != "warn" || check.Message != "context execution unavailable" {
		t.Fatalf("check=%#v", check)
	}
	for _, required := range []string{"provider=unavailable", "shell_adapters=none", "helper_protocol=unavailable", "evidence_authority=unavailable", "blockers=delegated_provider_unavailable,privacy_topology_unqualified,context_exec_runtime_unavailable"} {
		if !strings.Contains(check.Hint, required) {
			t.Fatalf("missing blocker %q in hint %q", required, check.Hint)
		}
	}
}
