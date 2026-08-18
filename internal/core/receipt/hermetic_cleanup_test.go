package receipt

import "testing"

func TestHermeticCleanupValidationIsBoundedAndModernNonPersistentOnly(t *testing.T) {
	cleanup := &HermeticCleanup{Status: HermeticCleanupIncomplete, Reason: "discard_failed"}
	if err := cleanup.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []HermeticCleanup{
		{}, {Status: "complete", Reason: "discard_failed"}, {Status: HermeticCleanupIncomplete, Reason: "/private/hb_secret"},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid hermetic cleanup accepted: %#v", invalid)
		}
	}
	for _, version := range []int{1, 4} {
		rec := Receipt{SchemaVersion: version, HermeticCleanup: cleanup}
		if err := rec.Validate(); err == nil {
			t.Fatalf("receipt v%d accepted hermetic cleanup metadata", version)
		}
	}
}
