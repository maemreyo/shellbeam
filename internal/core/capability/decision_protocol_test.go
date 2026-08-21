package capability

import (
	"reflect"
	"testing"
)

func TestDecisionProtocolCapabilityAdvertisesClosedV1Contract(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureDecisionProtocol] != Unavailable || base.DecisionProtocol != nil {
		t.Fatalf("baseline overclaims decision protocol: %#v", base.DecisionProtocol)
	}
	support := DecisionProtocolSupport{
		SchemaVersion:             1,
		ProtocolVersion:           1,
		PredicateKinds:            []string{"OPERATION_OUTCOME", "STRUCTURED_TEST_STATUS", "STRUCTURED_DIAGNOSTIC_PRESENCE", "VERIFICATION_RESULT"},
		AuthorityProviders:        []string{"shellbeam.explicit_caller.v1"},
		OneExecutionPerExperiment: true,
	}
	got := base.WithDecisionProtocol(support)
	if got.Features[FeatureDecisionProtocol] != Available || got.DecisionProtocol == nil || !reflect.DeepEqual(*got.DecisionProtocol, support) {
		t.Fatalf("decision protocol not advertised exactly: %#v", got.DecisionProtocol)
	}
	clone := got.Clone()
	clone.DecisionProtocol.PredicateKinds[0] = "FORGED"
	clone.DecisionProtocol.AuthorityProviders[0] = "forged"
	if got.DecisionProtocol.PredicateKinds[0] != "OPERATION_OUTCOME" || got.DecisionProtocol.AuthorityProviders[0] != "shellbeam.explicit_caller.v1" {
		t.Fatal("catalog clone aliased decision protocol slices")
	}
}

func TestDecisionProtocolCapabilityRejectsOpenOrInvalidContract(t *testing.T) {
	valid := DecisionProtocolSupport{
		SchemaVersion:             1,
		ProtocolVersion:           1,
		PredicateKinds:            []string{"OPERATION_OUTCOME", "STRUCTURED_TEST_STATUS", "STRUCTURED_DIAGNOSTIC_PRESENCE", "VERIFICATION_RESULT"},
		AuthorityProviders:        []string{"shellbeam.explicit_caller.v1"},
		OneExecutionPerExperiment: true,
	}
	cases := []DecisionProtocolSupport{
		{},
		{SchemaVersion: 2, ProtocolVersion: 1, PredicateKinds: valid.PredicateKinds, OneExecutionPerExperiment: true},
		{SchemaVersion: 1, ProtocolVersion: 2, PredicateKinds: valid.PredicateKinds, OneExecutionPerExperiment: true},
		{SchemaVersion: 1, ProtocolVersion: 1, PredicateKinds: []string{"OPERATION_OUTCOME"}, OneExecutionPerExperiment: true},
		{SchemaVersion: 1, ProtocolVersion: 1, PredicateKinds: valid.PredicateKinds, OneExecutionPerExperiment: false},
		{SchemaVersion: 1, ProtocolVersion: 1, PredicateKinds: valid.PredicateKinds, AuthorityProviders: []string{""}, OneExecutionPerExperiment: true},
	}
	for i, support := range cases {
		got := Baseline(Limits{}).WithDecisionProtocol(support)
		if got.Features[FeatureDecisionProtocol] != Unavailable || got.DecisionProtocol != nil {
			t.Fatalf("case %d advertised invalid support: %#v", i, got.DecisionProtocol)
		}
	}
}
