package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	control "github.com/maemreyo/shellbeam/internal/app/control"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

type terminalProviderFailureReason string

const (
	terminalProviderNotRunning          terminalProviderFailureReason = "not_running"
	terminalProviderProbeFailed         terminalProviderFailureReason = "probe_failed"
	terminalProviderPlatformUnsupported terminalProviderFailureReason = "platform_unsupported"
)

type terminalProviderDiagnostic struct {
	ProviderID    string
	Available     bool
	FailureReason terminalProviderFailureReason
}

type terminalPresentationDiagnostics struct {
	Providers     []terminalProviderDiagnostic
	FailureReason terminalProviderFailureReason
}

func appendInteractiveHandoffDoctorChecks(checks []control.Check, socket control.Check, socketPath string) []control.Check {
	checks = append(checks, doctorHostTerminalPresentationCheck(context.Background()))
	handoffCatalog := capability.Baseline(capability.Limits{})
	if socket.Status == control.Pass {
		handoffCatalog = doctorInteractiveHandoffCatalog(socketPath)
	}
	checks = append(checks, doctorInteractiveHandoffCheck(handoffCatalog))
	checks = append(checks, doctorContextExecCheck(handoffCatalog))
	return checks
}

func doctorInteractiveHandoffCatalog(socket string) capability.Catalog {
	catalog := capability.Baseline(capability.Limits{})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	resp, err := ipcadapter.NewClient(socket).CallV2(ctx, ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "doctor-interactive-handoff", Action: "inspect.server"})
	if err != nil || !resp.OK || resp.Server == nil {
		return catalog
	}
	return resp.Server.Clone()
}

func doctorContextExecCheck(catalog capability.Catalog) control.Check {
	check := control.Check{ID: "context_exec", Status: control.Warn, Message: "context execution unavailable"}
	provider := "unavailable"
	if catalog.Features[capability.FeatureDelegatedInteractive] == capability.Available && catalog.DelegatedInteractive != nil {
		provider = catalog.DelegatedInteractive.ProviderID
	}
	shellAdapters := "none"
	helperProtocol := "unavailable"
	evidenceAuthority := "unavailable"
	if catalog.ContextExec != nil {
		if len(catalog.ContextExec.ShellAdapters) > 0 {
			shells := make([]string, len(catalog.ContextExec.ShellAdapters))
			for i, family := range catalog.ContextExec.ShellAdapters {
				shells[i] = string(family)
			}
			shellAdapters = strings.Join(shells, ",")
		}
		if catalog.ContextExec.HelperProtocolVersion > 0 {
			helperProtocol = fmt.Sprintf("%d", catalog.ContextExec.HelperProtocolVersion)
		}
		if catalog.ContextExec.EvidenceAuthority != "" {
			evidenceAuthority = catalog.ContextExec.EvidenceAuthority
		}
	}

	blockers := make([]string, 0, 3)
	if catalog.Features[capability.FeatureDelegatedInteractive] != capability.Available || catalog.DelegatedInteractive == nil {
		blockers = append(blockers, "delegated_provider_unavailable")
	}
	privacyQualified := catalog.Features[capability.FeatureInteractiveHandoff] == capability.Available &&
		catalog.InteractiveHandoff != nil &&
		catalog.InteractiveHandoff.Secret &&
		catalog.InteractiveHandoff.Privacy != nil &&
		catalog.InteractiveHandoff.Privacy.SecretPrivateInterval &&
		catalog.InteractiveHandoff.Privacy.PrivacyReleaseSeparate &&
		catalog.InteractiveHandoff.Privacy.ObserverTopologyQualified &&
		!catalog.InteractiveHandoff.Privacy.HumanInputPersisted
	if !privacyQualified {
		blockers = append(blockers, "privacy_topology_unqualified")
	}
	if catalog.Features[capability.FeatureContextExec] != capability.Available || catalog.ContextExec == nil {
		blockers = append(blockers, "context_exec_runtime_unavailable")
	}

	parts := []string{"provider=" + provider, "shell_adapters=" + shellAdapters, "helper_protocol=" + helperProtocol, "evidence_authority=" + evidenceAuthority}
	if catalog.Features[capability.FeatureContextExec] == capability.Available && catalog.ContextExec != nil && len(blockers) == 0 {
		qualities := make([]string, len(catalog.ContextExec.EvidenceQualities))
		for i, quality := range catalog.ContextExec.EvidenceQualities {
			qualities[i] = string(quality)
		}
		parts = append(parts,
			"evidence_qualities="+strings.Join(qualities, ","),
			"output_attribution="+string(catalog.ContextExec.OutputAttribution),
			"resource_enforcement="+string(catalog.ContextExec.ResourceEnforcement),
			"isolated_hermetic="+string(catalog.ContextExec.Hermetic),
			"blockers=none",
		)
		check.Status = control.Pass
		check.Message = "context execution available"
		check.Hint = strings.Join(parts, "; ")
		return check
	}
	parts = append(parts, "blockers="+strings.Join(blockers, ","))
	check.Hint = strings.Join(parts, "; ")
	return check
}

func doctorInteractiveHandoffCheck(catalog capability.Catalog) control.Check {
	check := control.Check{ID: "interactive_handoff", Status: control.Warn, Message: "interactive handoff unavailable"}
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.InteractiveHandoff == nil || catalog.DelegatedInteractive == nil {
		check.Hint = "provider=unavailable; secret=unavailable; shell_integrations=none; privacy_topology=unqualified"
		return check
	}
	support := catalog.InteractiveHandoff
	parts := []string{"provider=" + catalog.DelegatedInteractive.ProviderID}
	if support.Secret {
		parts = append(parts, "secret=available")
	} else {
		parts = append(parts, "secret=unavailable")
	}
	if support.Privacy != nil && support.Privacy.ObserverTopologyQualified {
		parts = append(parts, "privacy_topology=qualified")
	} else {
		parts = append(parts, "privacy_topology=unqualified")
	}
	for _, integration := range support.ShellIntegrations {
		parts = append(parts, fmt.Sprintf("%s=%s", integration.Shell, integration.Level))
	}
	if len(support.RequirementKinds) > 0 {
		requirements := make([]string, len(support.RequirementKinds))
		for i, kind := range support.RequirementKinds {
			requirements[i] = string(kind)
		}
		parts = append(parts, "requirements="+strings.Join(requirements, ","))
	}
	if len(support.CaptureQualities) > 0 {
		qualities := make([]string, len(support.CaptureQualities))
		for i, quality := range support.CaptureQualities {
			qualities[i] = string(quality)
		}
		parts = append(parts, "capture="+strings.Join(qualities, ","))
	}
	check.Status = control.Pass
	check.Message = "interactive handoff available"
	check.Hint = strings.Join(parts, "; ")
	return check
}

func doctorTerminalPresentationCheck(diagnostics terminalPresentationDiagnostics) control.Check {
	check := control.Check{ID: "terminal_presentation", Status: control.Warn, Message: "automatic terminal presentation unavailable"}
	if len(diagnostics.Providers) == 0 {
		reason := diagnostics.FailureReason
		if reason == "" {
			reason = terminalProviderProbeFailed
		}
		check.Hint = fmt.Sprintf("providers=none; reason=%s; freshness_sources=none", reason)
		return check
	}

	providers := append([]terminalProviderDiagnostic(nil), diagnostics.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID < providers[j].ProviderID })
	parts := make([]string, 0, len(providers))
	available := false
	for _, provider := range providers {
		if provider.Available {
			available = true
			parts = append(parts, provider.ProviderID+":available")
			continue
		}
		reason := provider.FailureReason
		if reason == "" {
			reason = terminalProviderProbeFailed
		}
		parts = append(parts, fmt.Sprintf("%s:unavailable(reason=%s)", provider.ProviderID, reason))
	}
	if available {
		check.Status = control.Pass
		check.Message = "automatic terminal presentation available"
	}
	check.Hint = fmt.Sprintf(
		"providers=%s; freshness_sources=active=%s,recent=%s,bridge_affinity=request_bound,single_running=%s",
		strings.Join(parts, ","), terminalActiveFreshness, terminalRecentFreshness, terminalRunningFreshness,
	)
	return check
}
