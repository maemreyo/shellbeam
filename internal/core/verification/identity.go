package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type RelationIdentityInput struct {
	From                Subject             `json:"from"`
	To                  Subject             `json:"to"`
	Kind                string              `json:"kind"`
	Basis               RelationBasis       `json:"basis"`
	DerivationAuthority DerivationAuthority `json:"derivation_authority"`
	Coverage            Coverage            `json:"coverage"`
	Provider            *ProviderRef        `json:"provider,omitempty"`
	SourceGeneration    string              `json:"source_generation"`
	ProvenanceRefs      []string            `json:"provenance_refs"`
}

func hashID(prefix string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return prefix + hex.EncodeToString(sum[:]), nil
}
func normalizeRefs(in []string) ([]string, error) {
	if err := validateRefs(in, 128, 2048); err != nil {
		return nil, err
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, v := range out {
		if n == 0 || out[n-1] != v {
			out[n] = v
			n++
		}
	}
	return out[:n], nil
}
func PolicyDigest(content PolicyContent) (string, error) {
	if err := content.Validate(); err != nil {
		return "", err
	}
	return hashID("pol_", canonicalPolicy(content))
}
func DomainID(kind AffectedDomainKind, provider *ProviderRef, generation string, provenanceRefs []string) (string, error) {
	if kind.Validate() != nil || !validGeneration(generation) {
		return "", fmt.Errorf("invalid domain identity")
	}
	if provider != nil {
		if err := provider.Validate(); err != nil {
			return "", err
		}
	}
	refs, err := normalizeRefs(provenanceRefs)
	if err != nil {
		return "", err
	}
	return hashID("dom_", struct {
		Kind       AffectedDomainKind `json:"kind"`
		Provider   *ProviderRef       `json:"provider,omitempty"`
		Generation string             `json:"generation"`
		Refs       []string           `json:"provenance_refs"`
	}{kind, provider, generation, refs})
}
func RelationID(in RelationIdentityInput) (string, error) {
	if in.From.Validate() != nil || in.To.Validate() != nil || !boundedToken(in.Kind, 128) || in.Basis.Validate() != nil || in.DerivationAuthority.Validate() != nil || in.Coverage.Validate() != nil || !validGeneration(in.SourceGeneration) {
		return "", fmt.Errorf("invalid relation identity")
	}
	if in.Provider != nil {
		if err := in.Provider.Validate(); err != nil {
			return "", err
		}
	}
	refs, err := normalizeRefs(in.ProvenanceRefs)
	if err != nil {
		return "", err
	}
	in.ProvenanceRefs = refs
	return hashID("rel_", in)
}
func ObligationID(policyDigest, ruleID, generation string, triggerRefs []string) (string, error) {
	return referenceSetID("obl_", policyDigest, ruleID, generation, triggerRefs)
}
func PolicyGapID(policyDigest, classID, generation string, surfaceRefs []string) (string, error) {
	return referenceSetID("gap_", policyDigest, classID, generation, surfaceRefs)
}
func referenceSetID(prefix, digest, semanticID, generation string, refs []string) (string, error) {
	if !isDerivedID(digest, "pol_") || !boundedToken(semanticID, 128) || !validGeneration(generation) {
		return "", fmt.Errorf("invalid semantic identity")
	}
	normalized, err := normalizeRefs(refs)
	if err != nil {
		return "", err
	}
	return hashID(prefix, struct {
		PolicyDigest string   `json:"policy_digest"`
		SemanticID   string   `json:"semantic_id"`
		Generation   string   `json:"generation"`
		Refs         []string `json:"refs"`
	}{digest, semanticID, generation, normalized})
}
func isDerivedID(v, prefix string) bool {
	if !strings.HasPrefix(v, prefix) || len(v) != len(prefix)+64 {
		return false
	}
	_, err := hex.DecodeString(v[len(prefix):])
	return err == nil
}
