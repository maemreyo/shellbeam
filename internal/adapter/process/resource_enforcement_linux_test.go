//go:build linux

package process

import "testing"

func TestResourceEnforcementLinuxAbsentConfigurationDoesNoProviderWork(t *testing.T) {
	t.Setenv(resourceCgroupRootEnv, "")
	owner, support, err := NewOwnerFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if support != nil || owner.resources != nil {
		t.Fatalf("absent config owner=%#v support=%#v", owner, support)
	}
}
