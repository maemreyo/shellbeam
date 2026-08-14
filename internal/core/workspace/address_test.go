package workspace

import "testing"

func TestAddressValidatesExactlyOneAbsoluteOrWorkspaceRelativeForm(t *testing.T) {
	ws := WorkspaceID("ws_01K00000000000000000000000")
	valid := []Address{{CWD: "/repo/src"}, {WorkspaceID: ws}, {WorkspaceID: ws, CWD: "."}, {WorkspaceID: ws, CWD: "src/pkg"}}
	for _, address := range valid {
		if err := address.Validate(); err != nil {
			t.Fatalf("Validate(%#v): %v", address, err)
		}
	}
	invalid := []Address{{}, {CWD: "relative"}, {WorkspaceID: ws, CWD: "/absolute"}, {WorkspaceID: ws, CWD: "../escape"}, {WorkspaceID: ws, CWD: "a/../escape"}, {WorkspaceID: ws, CWD: "bad\x00path"}}
	for _, address := range invalid {
		if err := address.Validate(); err == nil {
			t.Fatalf("Validate(%#v) unexpectedly succeeded", address)
		}
	}
}

func TestAddressLogicalCWDDefaultsToDot(t *testing.T) {
	address := Address{WorkspaceID: WorkspaceID("ws_01K00000000000000000000000")}
	if got := address.LogicalCWD(); got != "." {
		t.Fatalf("LogicalCWD=%q", got)
	}
}
