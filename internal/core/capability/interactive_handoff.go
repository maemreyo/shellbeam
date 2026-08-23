package capability

import (
	receipt "github.com/maemreyo/shellbeam/internal/core/receipt"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ShellIntegrationSupport struct {
	Shell                       shell.ShellFamily     `json:"shell"`
	Level                       shell.CapabilityLevel `json:"level"`
	SafeBoundary                bool                  `json:"safe_boundary"`
	EnvironmentExportedNonempty bool                  `json:"environment_exported_nonempty"`
}

func (s ShellIntegrationSupport) valid() bool {
	if s.Shell != shell.ShellFish && s.Shell != shell.ShellZsh && s.Shell != shell.ShellBash && s.Shell != shell.ShellNushell {
		return false
	}
	if err := s.Level.Validate(); err != nil {
		return false
	}
	return s.SafeBoundary && s.EnvironmentExportedNonempty &&
		(s.Level == shell.CapabilityRequirementAware || s.Level == shell.CapabilityFullHandoff)
}

type HandoffPrivacySupport struct {
	SecretPrivateInterval     bool `json:"secret_private_interval"`
	PrivacyReleaseSeparate    bool `json:"privacy_release_separate"`
	ObserverTopologyQualified bool `json:"observer_topology_qualified"`
	HumanInputPersisted       bool `json:"human_input_persisted"`
}

func (s HandoffPrivacySupport) valid() bool {
	return s.SecretPrivateInterval && s.PrivacyReleaseSeparate && s.ObserverTopologyQualified && !s.HumanInputPersisted
}

type InteractiveHandoffSupport struct {
	ManualStandard       bool                         `json:"manual_standard"`
	Secret               bool                         `json:"secret"`
	AutomaticReadiness   bool                         `json:"automatic_readiness"`
	ShellIntegrations    []ShellIntegrationSupport    `json:"shell_integrations,omitempty"`
	RequirementKinds     []shell.RequirementKind      `json:"requirement_kinds,omitempty"`
	Privacy              *HandoffPrivacySupport       `json:"privacy,omitempty"`
	CaptureQualities     []receipt.CaptureQuality     `json:"capture_qualities,omitempty"`
	TerminalPresentation *TerminalPresentationSupport `json:"terminal_presentation,omitempty"`
}

func (s InteractiveHandoffSupport) ValidH2() bool {
	return s.ManualStandard && !s.Secret && !s.AutomaticReadiness && len(s.ShellIntegrations) == 0 &&
		len(s.RequirementKinds) == 0 && s.Privacy == nil && len(s.CaptureQualities) == 0
}

func (s InteractiveHandoffSupport) ValidH4() bool {
	if !s.ManualStandard || (!s.Secret && !s.AutomaticReadiness) {
		return false
	}
	if !validCaptureQualities(s.CaptureQualities) {
		return false
	}
	if s.Secret {
		if s.Privacy == nil || !s.Privacy.valid() {
			return false
		}
	} else if s.Privacy != nil {
		return false
	}
	if s.AutomaticReadiness {
		if !validShellIntegrations(s.ShellIntegrations) || len(s.RequirementKinds) != 1 || s.RequirementKinds[0] != shell.RequirementEnvironmentExportedNonempty {
			return false
		}
	} else if len(s.ShellIntegrations) != 0 || len(s.RequirementKinds) != 0 {
		return false
	}
	return true
}

func validShellIntegrations(values []ShellIntegrationSupport) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[shell.ShellFamily]struct{}, len(values))
	for _, value := range values {
		if !value.valid() {
			return false
		}
		if _, ok := seen[value.Shell]; ok {
			return false
		}
		seen[value.Shell] = struct{}{}
	}
	return true
}

func validCaptureQualities(values []receipt.CaptureQuality) bool {
	if len(values) != 3 {
		return false
	}
	want := []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete}
	for i := range want {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
