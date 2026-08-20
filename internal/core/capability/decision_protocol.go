package capability

import "slices"

const FeatureDecisionProtocol Feature = "decision_protocol"

var decisionProtocolV1PredicateKinds = []string{
	"OPERATION_OUTCOME",
	"STRUCTURED_TEST_STATUS",
	"STRUCTURED_DIAGNOSTIC_PRESENCE",
	"VERIFICATION_RESULT",
}

type DecisionProtocolSupport struct {
	SchemaVersion             int      `json:"schema_version"`
	ProtocolVersion           int      `json:"protocol_version"`
	PredicateKinds            []string `json:"predicate_kinds"`
	AuthorityProviders        []string `json:"authority_providers,omitempty"`
	OneExecutionPerExperiment bool     `json:"one_execution_per_experiment"`
}

func (s DecisionProtocolSupport) ValidV1() bool {
	if s.SchemaVersion != 1 || s.ProtocolVersion != 1 || !s.OneExecutionPerExperiment || !slices.Equal(s.PredicateKinds, decisionProtocolV1PredicateKinds) {
		return false
	}
	for _, provider := range s.AuthorityProviders {
		if provider == "" || len(provider) > 128 {
			return false
		}
	}
	return true
}

func (s DecisionProtocolSupport) clone() DecisionProtocolSupport {
	out := s
	out.PredicateKinds = append([]string(nil), s.PredicateKinds...)
	out.AuthorityProviders = append([]string(nil), s.AuthorityProviders...)
	return out
}

func (c Catalog) WithDecisionProtocol(s DecisionProtocolSupport) Catalog {
	out := c.Clone()
	if !s.ValidV1() {
		return out
	}
	out.Features[FeatureDecisionProtocol] = Available
	copy := s.clone()
	out.DecisionProtocol = &copy
	return out
}

func cloneDecisionProtocolSupport(in *DecisionProtocolSupport) *DecisionProtocolSupport {
	if in == nil {
		return nil
	}
	out := in.clone()
	return &out
}
