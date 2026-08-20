package verification

import "testing"

func TestCostMetricUnavailableNeverPretendsZero(t *testing.T) {
	if err := (CostMetric{Quality: CostQualityUnavailable}).Validate(); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := (CostMetric{Quality: CostQualityUnavailable, Latest: &zero}).Validate(); err == nil {
		t.Fatal("unavailable cost accepted a value")
	}
}

func TestBoundRequirementCostPreservesExecutionSemantics(t *testing.T) {
	parallel := false
	v := BoundRequirementCost{ObligationID: "obl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RequirementID: "integration", ProviderClass: ProviderIntegrationTest, Execution: ProviderExecutionSemantics{ParallelSafe: &parallel, SharedResources: []string{"db"}, ExclusiveResourceClass: "database", ExpectedWorkloadClass: "heavy"}, Cost: UnavailableVerificationCost()}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	if v.Execution.ParallelSafe == nil || *v.Execution.ParallelSafe || len(v.Execution.SharedResources) != 1 {
		t.Fatalf("execution semantics lost: %#v", v.Execution)
	}
}

func TestBoundRequirementCostRejectsInvalidExecutionAndCommandDrift(t *testing.T) {
	base := BoundRequirementCost{ObligationID: "obl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RequirementID: "integration", ProviderClass: ProviderIntegrationTest, Cost: UnavailableVerificationCost()}
	badExecution := base
	badExecution.Execution.ExpectedWorkloadClass = "invented"
	if err := badExecution.Validate(); err == nil {
		t.Fatal("invalid execution semantics accepted")
	}
	badCommand := base
	badCommand.ProjectCommandID = "test_full"
	badCommand.Cost.ProjectCommandID = "test_other"
	if err := badCommand.Validate(); err == nil {
		t.Fatal("bound/cost project command drift accepted")
	}
}
