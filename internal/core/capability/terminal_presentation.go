package capability

import (
	receipt "github.com/maemreyo/shellbeam/internal/core/receipt"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type TerminalPresentationSupport struct {
	ResolutionSources  []string `json:"resolution_sources,omitempty"`
	QualifiedLaunchers []string `json:"qualified_launchers,omitempty"`
}

func (s TerminalPresentationSupport) Valid() bool {
	return len(s.ResolutionSources) > 0 && len(s.QualifiedLaunchers) > 0 && allNonEmptyUnique(s.ResolutionSources) && allNonEmptyUnique(s.QualifiedLaunchers)
}

func (s InteractiveHandoffSupport) Clone() InteractiveHandoffSupport {
	out := s
	out.ShellIntegrations = append([]ShellIntegrationSupport(nil), s.ShellIntegrations...)
	out.RequirementKinds = append([]shell.RequirementKind(nil), s.RequirementKinds...)
	out.CaptureQualities = append([]receipt.CaptureQuality(nil), s.CaptureQualities...)
	if s.Privacy != nil {
		privacy := *s.Privacy
		out.Privacy = &privacy
	}
	if s.TerminalPresentation != nil {
		presentation := *s.TerminalPresentation
		presentation.ResolutionSources = append([]string(nil), presentation.ResolutionSources...)
		presentation.QualifiedLaunchers = append([]string(nil), presentation.QualifiedLaunchers...)
		out.TerminalPresentation = &presentation
	}
	return out
}

func (c Catalog) WithTerminalPresentation(support TerminalPresentationSupport) Catalog {
	out := c.Clone()
	if !support.Valid() || out.Features[FeatureInteractiveHandoff] != Available || out.InteractiveHandoff == nil {
		return out
	}
	copy := support
	copy.ResolutionSources = append([]string(nil), support.ResolutionSources...)
	copy.QualifiedLaunchers = append([]string(nil), support.QualifiedLaunchers...)
	out.InteractiveHandoff.TerminalPresentation = &copy
	return out
}

func allNonEmptyUnique(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
