package capability

import (
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ContextExecSupport struct {
	ProviderID            string                        `json:"provider_id"`
	ProviderVersion       int                           `json:"provider_version"`
	Platform              string                        `json:"platform"`
	ShellAdapters         []shellcore.ShellFamily       `json:"shell_adapters"`
	HelperProtocolVersion int                           `json:"helper_protocol_version"`
	EvidenceAuthority     string                        `json:"evidence_authority"`
	EvidenceQualities     []contextcore.EvidenceQuality `json:"evidence_qualities"`
	OutputAttribution     contextcore.OutputAttribution `json:"output_attribution"`
	ResourceEnforcement   Availability                  `json:"resource_enforcement"`
	Hermetic              Availability                  `json:"hermetic"`
}

func (s ContextExecSupport) Clone() ContextExecSupport {
	out := s
	out.ShellAdapters = append([]shellcore.ShellFamily(nil), s.ShellAdapters...)
	out.EvidenceQualities = append([]contextcore.EvidenceQuality(nil), s.EvidenceQualities...)
	return out
}

func (s ContextExecSupport) valid() bool {
	if s.ProviderID == "" || s.ProviderVersion < 1 || s.Platform == "" || s.HelperProtocolVersion < 1 {
		return false
	}
	if len(s.ShellAdapters) != 3 || s.ShellAdapters[0] != shellcore.ShellFish || s.ShellAdapters[1] != shellcore.ShellZsh || s.ShellAdapters[2] != shellcore.ShellBash {
		return false
	}
	wantQualities := []contextcore.EvidenceQuality{
		contextcore.EvidenceQualityUnproven,
		contextcore.EvidenceQualityIncomplete,
		contextcore.EvidenceQualityComplete,
		contextcore.EvidenceQualityAmbiguous,
	}
	if len(s.EvidenceQualities) != len(wantQualities) {
		return false
	}
	for i := range wantQualities {
		if s.EvidenceQualities[i] != wantQualities[i] {
			return false
		}
	}
	return s.EvidenceAuthority == contextcore.EvidenceAuthorityContextExecChildOwnedV1 &&
		s.OutputAttribution == contextcore.OutputAttributionHelperOwnedChildPipes &&
		s.ResourceEnforcement == Unavailable && s.Hermetic == Unavailable
}

func (c Catalog) WithContextExec(support ContextExecSupport) Catalog {
	out := c.Clone()
	if !support.valid() || out.Features[FeatureDelegatedInteractive] != Available || out.DelegatedInteractive == nil ||
		out.Features[FeatureInteractiveHandoff] != Available || out.InteractiveHandoff == nil || !out.InteractiveHandoff.ValidH4() ||
		!out.InteractiveHandoff.Secret || out.InteractiveHandoff.Privacy == nil || !out.InteractiveHandoff.Privacy.valid() {
		return out
	}
	delegated := out.DelegatedInteractive
	if support.ProviderID != delegated.ProviderID || support.ProviderVersion != delegated.ProviderVersion || support.Platform != delegated.Platform {
		return out
	}
	out.Features[FeatureContextExec] = Available
	copy := support.Clone()
	out.ContextExec = &copy
	foundV6 := false
	for _, version := range out.ReceiptSchemaVersions {
		foundV6 = foundV6 || version == 6
	}
	if !foundV6 {
		out.ReceiptSchemaVersions = append(out.ReceiptSchemaVersions, 6)
	}
	return out
}
