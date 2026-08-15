package operation

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

func TestReservationEvidenceEligibleUsesOnlyFrozenMetadata(t *testing.T) {
	cases := []struct {
		name        string
		reservation Reservation
		want        bool
	}{
		{"plain", Reservation{}, false},
		{"explicit", Reservation{Evidence: &evidence.Contract{VerificationKind: evidence.VerificationTest}}, true},
		{"declared test", Reservation{Intent: &DeclaredIntent{Kind: IntentKindTest}}, true},
		{"declared inspect", Reservation{Intent: &DeclaredIntent{Kind: IntentKindInspect}}, false},
		{"typed v2 build", Reservation{ProjectCommand: &project.CommandBinding{SchemaVersion: project.BindingSchemaVersion, Kind: "build"}}, true},
		{"typed v2 artifact", Reservation{ProjectCommand: &project.CommandBinding{SchemaVersion: project.BindingSchemaVersion, Kind: "inspect", ExpectedOutputs: []project.Output{{Path: "x", Kind: "file", Required: true}}}}, true},
		{"typed legacy", Reservation{ProjectCommand: &project.CommandBinding{SchemaVersion: project.BindingSchemaV1}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reservation.EvidenceEligible(); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
