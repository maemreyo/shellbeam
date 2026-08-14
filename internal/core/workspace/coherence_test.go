package workspace

import "testing"

func TestCoherenceBarrierValidate(t *testing.T) {
	valid := CoherenceBarrier{DaemonIncarnation: "daemon-1", Epoch: 0, ActiveManagedShellOperations: 0}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid barrier: %v", err)
	}
	cases := []CoherenceBarrier{
		{Epoch: 1, ActiveManagedShellOperations: 0},
		{DaemonIncarnation: "daemon-1", Epoch: 1, ActiveManagedShellOperations: -1},
	}
	for i, barrier := range cases {
		if err := barrier.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %#v", i, barrier)
		}
	}
}
