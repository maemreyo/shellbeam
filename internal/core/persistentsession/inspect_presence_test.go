package persistentsession

import "testing"

func TestInspectRequestDistinguishesOmittedPersistentOnlyFromExplicitFalse(t *testing.T) {
	request := InspectRequest{}
	if request.PersistentOnly != nil {
		t.Fatalf("omitted persistent_only=%v", *request.PersistentOnly)
	}
	value := false
	request.PersistentOnly = &value
	if request.PersistentOnly == nil || *request.PersistentOnly {
		t.Fatalf("explicit persistent_only=%v", request.PersistentOnly)
	}
}
