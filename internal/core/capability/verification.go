package capability

type VerificationSemanticsSupport struct {
	SchemaVersions                 []int `json:"schema_versions,omitempty"`
	PolicySchemaVersions           []int `json:"policy_schema_versions,omitempty"`
	MaxDomains                     int   `json:"max_domains,omitempty"`
	MaxRelations                   int   `json:"max_relations,omitempty"`
	MaxObligations                 int   `json:"max_obligations,omitempty"`
	MaxPolicyGaps                  int   `json:"max_policy_gaps,omitempty"`
	MaxPolicyRules                 int   `json:"max_policy_rules,omitempty"`
	MaxClassifications             int   `json:"max_classifications,omitempty"`
	MaxEvidenceRequirementsPerRule int   `json:"max_evidence_requirements_per_rule,omitempty"`
}

func (s VerificationSemanticsSupport) ValidV1() bool {
	return len(s.SchemaVersions) == 1 && s.SchemaVersions[0] == 1 && s.validPolicyAndLimits()
}

func (s VerificationSemanticsSupport) Valid() bool {
	if !s.validPolicyAndLimits() {
		return false
	}
	return (len(s.SchemaVersions) == 1 && s.SchemaVersions[0] == 1) ||
		(len(s.SchemaVersions) == 2 && s.SchemaVersions[0] == 1 && s.SchemaVersions[1] == 2)
}

func (s VerificationSemanticsSupport) validPolicyAndLimits() bool {
	return len(s.PolicySchemaVersions) == 1 && s.PolicySchemaVersions[0] == 1 && s.MaxDomains == 16 && s.MaxRelations == 512 && s.MaxObligations == 256 && s.MaxPolicyGaps == 128 && s.MaxPolicyRules == 128 && s.MaxClassifications == 128 && s.MaxEvidenceRequirementsPerRule == 32
}
func (s VerificationSemanticsSupport) clone() VerificationSemanticsSupport {
	out := s
	out.SchemaVersions = append([]int(nil), s.SchemaVersions...)
	out.PolicySchemaVersions = append([]int(nil), s.PolicySchemaVersions...)
	return out
}
func (c Catalog) WithVerificationSemantics(s VerificationSemanticsSupport) Catalog {
	out := c.Clone()
	if !s.Valid() {
		return out
	}
	out.Features[FeatureVerificationSemantics] = Available
	copy := s.clone()
	out.VerificationSemantics = &copy
	return out
}
