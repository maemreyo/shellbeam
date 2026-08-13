package control

import "testing"

func TestDoctorStatuses(t *testing.T) {
	r := Report{SchemaVersion: 1, Checks: []Check{{ID: "config", Status: Pass}, {ID: "tunnel_client", Status: Warn}}}
	if r.ExitCode() != 0 {
		t.Fatalf("exit=%d", r.ExitCode())
	}
	r.Checks = append(r.Checks, Check{ID: "socket", Status: Fail})
	if r.ExitCode() != 1 {
		t.Fatalf("exit=%d", r.ExitCode())
	}
}
