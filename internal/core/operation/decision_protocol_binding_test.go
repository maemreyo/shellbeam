package operation

import "testing"

func TestDecisionProtocolExperimentIdentityChangesOnlyObservationBindingFingerprint(t *testing.T) {
	baseIntent := Intent{Command: "true", CWD: "/repo", ResolvedCWD: "/repo"}
	withExperiment := baseIntent
	withExperiment.ExperimentID = "exp-a"
	baseRequest, err := baseIntent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	experimentRequest, err := withExperiment.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if experimentRequest != baseRequest {
		t.Fatalf("experiment changed request fingerprint: %s != %s", experimentRequest, baseRequest)
	}
	baseExecution, err := baseIntent.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	experimentExecution, err := withExperiment.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if experimentExecution != baseExecution {
		t.Fatalf("experiment changed execution fingerprint: %s != %s", experimentExecution, baseExecution)
	}
	legacy, err := (ObservationBinding{ActivityID: "activity-dp"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	omitted, err := (ObservationBinding{ActivityID: "activity-dp", ExperimentID: ""}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	one, err := (ObservationBinding{ActivityID: "activity-dp", ExperimentID: "exp-a"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	two, err := (ObservationBinding{ActivityID: "activity-dp", ExperimentID: "exp-b"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if omitted != legacy {
		t.Fatalf("omitted experiment changed legacy observation fingerprint: %s != %s", omitted, legacy)
	}
	if one == legacy || two == legacy || one == two {
		t.Fatalf("observation fingerprints legacy=%q one=%q two=%q", legacy, one, two)
	}
}
