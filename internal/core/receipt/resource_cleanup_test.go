package receipt

import "testing"

func TestResourceCleanupValidationIsBoundedAndModernNonPersistentOnly(t *testing.T) {
	for _, reason := range []string{
		"final_events_unavailable", "cleanup_kill_failed", "cleanup_events_failed", "cleanup_events_invalid", "cleanup_timeout", "cleanup_remove_failed", "cleanup_unknown",
	} {
		if err := (ResourceCleanup{Status: ResourceCleanupIncomplete, Reason: reason}).Validate(); err != nil {
			t.Fatalf("reason %q rejected: %v", reason, err)
		}
	}
	for _, cleanup := range []ResourceCleanup{
		{}, {Status: "complete", Reason: "cleanup_remove_failed"}, {Status: ResourceCleanupIncomplete, Reason: "/private/cgroup"},
	} {
		if err := cleanup.Validate(); err == nil {
			t.Fatalf("invalid cleanup accepted: %#v", cleanup)
		}
	}
	cleanup := &ResourceCleanup{Status: ResourceCleanupIncomplete, Reason: "cleanup_remove_failed"}
	v1 := Receipt{SchemaVersion: 1, ResourceCleanup: cleanup}
	if err := v1.Validate(); err == nil {
		t.Fatal("v1 receipt accepted resource cleanup metadata")
	}
	v4 := Receipt{SchemaVersion: 4, ResourceCleanup: cleanup}
	if err := v4.Validate(); err == nil {
		t.Fatal("v4 persistent receipt accepted resource cleanup metadata")
	}
}
