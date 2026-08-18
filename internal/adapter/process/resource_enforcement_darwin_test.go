//go:build darwin

package process

import "testing"

func TestResourceEnforcementDarwinIsExplicitlyUnavailable(t *testing.T) {
	t.Setenv(resourceCgroupRootEnv, "/private/not-a-cgroup")
	owner, support, err := NewOwnerFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if support != nil {
		t.Fatalf("darwin advertised hard support: %#v", support)
	}
	if owner.resources != nil {
		t.Fatal("darwin owner retained a resource provider")
	}
}
