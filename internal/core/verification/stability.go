package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type CompatibilityKeyInput struct {
	ProviderClass                 string `json:"provider_class"`
	ProjectCommandID              string `json:"project_command_id,omitempty"`
	SourceGeneration              string `json:"source_generation,omitempty"`
	SourceContentDigest           string `json:"source_content_digest,omitempty"`
	ProjectBindingDigest          string `json:"project_binding_digest,omitempty"`
	SemanticContractDigest        string `json:"semantic_contract_digest"`
	EnvironmentFingerprint        string `json:"environment_fingerprint,omitempty"`
	EnvironmentFingerprintVersion int    `json:"environment_fingerprint_version,omitempty"`
	ToolchainFingerprint          string `json:"toolchain_fingerprint,omitempty"`
	ToolchainFingerprintVersion   int    `json:"toolchain_fingerprint_version,omitempty"`
}

type StabilityCounts struct {
	Runs       int `json:"runs"`
	Passes     int `json:"passes"`
	Failures   int `json:"failures"`
	Incomplete int `json:"incomplete"`
	Ambiguous  int `json:"ambiguous"`
}

type StabilityCohort struct {
	CompatibilityKey string          `json:"compatibility_key"`
	Status           EvidenceStatus  `json:"status"`
	Counts           StabilityCounts `json:"counts"`
	EvidenceRefs     []string        `json:"evidence_refs,omitempty"`
}

type FlakeEvaluation struct {
	ProtocolID            string          `json:"protocol_id"`
	CompatibilityKey      string          `json:"compatibility_key"`
	Counts                StabilityCounts `json:"counts"`
	QualifiedEvidenceRefs []string        `json:"qualified_evidence_refs,omitempty"`
}

type StabilityEvaluation struct {
	Status           EvidenceStatus    `json:"status"`
	Cohorts          []StabilityCohort `json:"cohorts,omitempty"`
	EvidenceRefs     []string          `json:"evidence_refs,omitempty"`
	ReasonCode       string            `json:"reason_code,omitempty"`
	DiagnosticReruns int               `json:"diagnostic_reruns,omitempty"`
	Flake            *FlakeEvaluation  `json:"flake,omitempty"`
}

func CompatibilityKey(candidate EvidenceCandidate) (string, bool) {
	if candidate.Validate() != nil || !candidate.ProviderClassKnown || candidate.ProviderClass.Validate() != nil || candidate.SourceGeneration == "" || candidate.SemanticContractDigest == "" {
		return "", false
	}
	if candidate.ProviderClass == ProviderProjectCommand && (candidate.ProjectCommandID == "" || candidate.ProjectBindingDigest == "") {
		return "", false
	}
	input := CompatibilityKeyInput{
		ProviderClass: string(candidate.ProviderClass), ProjectCommandID: candidate.ProjectCommandID,
		SourceGeneration: candidate.SourceGeneration, SourceContentDigest: candidate.SourceContentDigest,
		ProjectBindingDigest: candidate.ProjectBindingDigest, SemanticContractDigest: candidate.SemanticContractDigest,
		EnvironmentFingerprint: candidate.EnvironmentFingerprint, EnvironmentFingerprintVersion: candidate.EnvironmentFingerprintVersion,
		ToolchainFingerprint: candidate.ToolchainFingerprint, ToolchainFingerprintVersion: candidate.ToolchainFingerprintVersion,
	}
	return hashStabilityIdentity("compat_v1", input), true
}

func FlakeProtocolID(protocol FlakeProtocol) (string, bool) {
	if !validFlakeProtocol(protocol) {
		return "", false
	}
	return hashStabilityIdentity("flake_protocol_v1", protocol), true
}

func SortedEvidenceRefs(candidates []EvidenceCandidate) []string {
	ordered := append([]EvidenceCandidate(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CompletedAt.Equal(ordered[j].CompletedAt) {
			return ordered[i].EvidenceID < ordered[j].EvidenceID
		}
		return ordered[i].CompletedAt.Before(ordered[j].CompletedAt)
	})
	refs := make([]string, 0, len(ordered))
	for _, candidate := range ordered {
		refs = append(refs, candidate.EvidenceID)
	}
	return refs
}

func hashStabilityIdentity(kind string, value any) string {
	payload := struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
		Value   any    `json:"value"`
	}{Version: 1, Kind: kind, Value: value}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
