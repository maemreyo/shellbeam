package observation

import "testing"

func TestSnapshotIsBoundedAndTargeted(t *testing.T) {
	target := Target{Kind: TargetOperation, OperationID: "op-1"}
	valid := Snapshot{SchemaVersion: 1, Target: target, CapturedThroughSeq: 7, Facts: []SnapshotFact{{Code: "operation_state", Value: "running"}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tooMany := valid
	tooMany.Facts = make([]SnapshotFact, MaxSnapshotFacts+1)
	if err := tooMany.Validate(); err == nil {
		t.Fatal("unbounded snapshot facts accepted")
	}
	bad := valid
	bad.Facts = []SnapshotFact{{Code: "bad\ncode", Value: "x"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("control-bearing snapshot fact accepted")
	}
}

func TestCursorKeyMaterialRequiresTypedEpochAndGeneration(t *testing.T) {
	valid := CursorKeyMaterial{StateRootEpoch: "epoch_0123456789abcdef0123456789abcdef", Generation: "key_0123456789abcdef0123456789abcdef", Secret: make([]byte, 32)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []CursorKeyMaterial{
		{StateRootEpoch: "bad", Generation: valid.Generation, Secret: valid.Secret},
		{StateRootEpoch: valid.StateRootEpoch, Generation: "bad", Secret: valid.Secret},
		{StateRootEpoch: valid.StateRootEpoch, Generation: valid.Generation, Secret: make([]byte, 31)},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid cursor key accepted: %#v", invalid)
		}
	}
}
